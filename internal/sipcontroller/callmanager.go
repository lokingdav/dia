package sipcontroller

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dense-identity/denseid/internal/subscriber"
	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

// CallState represents the state of a call session
type CallState int

const (
	StateIdle CallState = iota
	StateDIAInitializing
	StateDIAAKEInProgress
	StateDIARUAInProgress
	StateDIAComplete
	StateBaresipDialing
	StateBaresipRinging
	StateBaresipEstablished
	StateODAActive
	StateHangingUp
	StateClosed
)

func (s CallState) String() string {
	names := []string{
		"Idle", "DIA_Initializing", "DIA_AKE_InProgress", "DIA_RUA_InProgress",
		"DIA_Complete", "Baresip_Dialing", "Baresip_Ringing", "Baresip_Established",
		"ODA_Active", "HangingUp", "Closed",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return "Unknown"
}

// CallSession represents a single call with its DIA and Baresip state
type CallSession struct {
	// Identity
	AttemptID  string // controller-generated attempt ID (unique per dial/invite)
	CallID     string // Baresip call-id
	AccountAOR string // Baresip account AOR handling the call (if provided)
	PeerPhone  string // E.164 phone number
	PeerURI    string // Full SIP URI
	PeerName   string // Display name if available
	Direction  string // "outgoing" or "incoming"

	// Experiment
	ProtocolEnabled bool
	DialSentAt      time.Time
	AnsweredAt      time.Time
	ODARequestedAt  time.Time
	ODACompletedAt  time.Time
	ODATimeout      *time.Timer

	// State
	State          CallState
	DIAComplete    bool
	SIPEstablished bool

	// Control flags
	BaselineHangupScheduled bool
	AnsweredResultEmitted   bool
	ODARequestScheduled     bool
	HangupAfterODARequested bool
	ODATerminalEmitted      bool
	ODATerminalOutcome      string
	ODATerminalError        string

	// DIA components
	DIAState      *dia.CallState
	DIAController *subscriber.Controller

	// Remote party info (from DIA)
	RemoteParty *dia.RemoteParty

	// Timing
	CreatedAt  time.Time
	DIATimeout *time.Timer

	// Error tracking
	LastError error

	// Synchronization
	mu sync.RWMutex
}

// NewCallSession creates a new call session
func NewCallSession(peerPhone, peerURI, direction string) *CallSession {
	return &CallSession{
		PeerPhone: peerPhone,
		PeerURI:   peerURI,
		Direction: direction,
		State:     StateIdle,
		CreatedAt: time.Now(),
	}
}

// SetState safely sets the call state
func (s *CallSession) SetState(state CallState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = state
}

// GetState safely gets the call state
func (s *CallSession) GetState() CallState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// IsOutgoing returns true if this is an outgoing call
func (s *CallSession) IsOutgoing() bool {
	return s.Direction == "outgoing"
}

// IsIncoming returns true if this is an incoming call
func (s *CallSession) IsIncoming() bool {
	return s.Direction == "incoming"
}

// CancelTimeout cancels the DIA timeout if set
func (s *CallSession) CancelTimeout() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DIATimeout != nil {
		s.DIATimeout.Stop()
		s.DIATimeout = nil
	}
}

// CancelODATimeout cancels the ODA timeout if set.
func (s *CallSession) CancelODATimeout() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ODATimeout != nil {
		s.ODATimeout.Stop()
		s.ODATimeout = nil
	}
}

// Cleanup releases resources associated with the session
func (s *CallSession) Cleanup() {
	s.CancelTimeout()
	s.CancelODATimeout()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.DIAController != nil {
		done := make(chan struct{})
		go func(ctrl *subscriber.Controller) {
			_ = ctrl.Close()
			close(done)
		}(s.DIAController)
		select {
		case <-done:
			// closed
		case <-time.After(500 * time.Millisecond):
			// Don't block shutdown on network teardown.
		}
		s.DIAController = nil
	}
	if s.DIAState != nil {
		s.DIAState.Close()
		s.DIAState = nil
	}
}

// CallManager manages multiple concurrent call sessions
type CallManager struct {
	attemptCounter atomic.Uint64

	byAttempt map[string]*CallSession // attemptID -> session
	byCallID  map[string]*CallSession // callID -> session

	// Outgoing calls are created before call-id is known.
	// We associate subsequent CALL_OUTGOING/CALL_RINGING events to the oldest
	// pending attempt for that peer.
	pendingByPeer map[string][]*CallSession // peerPhone -> FIFO queue of sessions

	mu sync.RWMutex
}

// GetActiveSessionByPeer returns the first non-closed session for a peer.
// This is used to avoid running multiple concurrent protocol sessions on the
// same deterministic DIA topic.
func (m *CallManager) GetActiveSessionByPeer(peerPhone string) *CallSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, s := range m.byAttempt {
		if s == nil {
			continue
		}
		if s.PeerPhone != peerPhone {
			continue
		}
		if s.GetState() == StateClosed {
			continue
		}
		return s
	}
	return nil
}

// NewCallManager creates a new call manager
func NewCallManager() *CallManager {
	return &CallManager{
		byAttempt:     make(map[string]*CallSession),
		byCallID:      make(map[string]*CallSession),
		pendingByPeer: make(map[string][]*CallSession),
	}
}

// NewOutgoingAttempt creates a new outgoing call attempt.
// It does not yet have a Baresip call-id; that will be bound when we observe
// CALL_OUTGOING (or another call event carrying the call-id).
func (m *CallManager) NewOutgoingAttempt(peerPhone, peerURI string, protocolEnabled bool) *CallSession {
	attemptID := fmt.Sprintf("a%d", m.attemptCounter.Add(1))
	session := NewCallSession(peerPhone, peerURI, "outgoing")
	session.AttemptID = attemptID
	session.ProtocolEnabled = protocolEnabled

	m.mu.Lock()
	defer m.mu.Unlock()

	m.byAttempt[attemptID] = session
	m.pendingByPeer[peerPhone] = append(m.pendingByPeer[peerPhone], session)

	return session
}

// NewIncomingSession creates and registers an incoming session with a known call-id.
func (m *CallManager) NewIncomingSession(callID, peerURI, peerName, accountAOR string) *CallSession {
	peerPhone := ExtractPhoneFromURI(peerURI)
	attemptID := fmt.Sprintf("a%d", m.attemptCounter.Add(1))
	session := NewCallSession(peerPhone, peerURI, "incoming")
	session.AttemptID = attemptID
	session.CallID = callID
	session.PeerName = peerName
	session.AccountAOR = accountAOR

	m.mu.Lock()
	defer m.mu.Unlock()

	m.byAttempt[attemptID] = session
	if callID != "" {
		m.byCallID[callID] = session
	}

	return session
}

// BindOutgoingCallID associates a call-id with the next pending outgoing attempt for the peer.
// Returns nil if no pending attempt exists.
func (m *CallManager) BindOutgoingCallID(peerPhone, callID, peerURI, accountAOR string) *CallSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue := m.pendingByPeer[peerPhone]
	if len(queue) == 0 {
		return nil
	}

	session := queue[0]
	queue = queue[1:]
	if len(queue) == 0 {
		delete(m.pendingByPeer, peerPhone)
	} else {
		m.pendingByPeer[peerPhone] = queue
	}

	session.CallID = callID
	session.PeerURI = peerURI
	session.AccountAOR = accountAOR
	if callID != "" {
		m.byCallID[callID] = session
	}

	return session
}

// GetByCallID retrieves a session by call ID
func (m *CallManager) GetByCallID(callID string) *CallSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byCallID[callID]
}

// GetByAttemptID retrieves a session by attempt ID.
func (m *CallManager) GetByAttemptID(attemptID string) *CallSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byAttempt[attemptID]
}

// RemoveSession removes a session from the manager
func (m *CallManager) RemoveSession(session *CallSession) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session.AttemptID != "" {
		delete(m.byAttempt, session.AttemptID)
	}
	if session.CallID != "" {
		delete(m.byCallID, session.CallID)
	}

	// If it was still pending, remove it from the peer queue.
	peer := session.PeerPhone
	if peer != "" {
		queue := m.pendingByPeer[peer]
		if len(queue) > 0 {
			filtered := queue[:0]
			for _, s := range queue {
				if s != session {
					filtered = append(filtered, s)
				}
			}
			if len(filtered) == 0 {
				delete(m.pendingByPeer, peer)
			} else {
				m.pendingByPeer[peer] = filtered
			}
		}
	}
}

// GetAllSessions returns all active sessions
func (m *CallManager) GetAllSessions() []*CallSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*CallSession, 0, len(m.byAttempt))
	for _, s := range m.byAttempt {
		result = append(result, s)
	}
	return result
}

// Count returns the number of active sessions
func (m *CallManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byAttempt)
}

// ExtractPhoneFromURI extracts phone number from SIP URI
// Examples:
//   - sip:+15551234567@domain.com -> +15551234567
//   - sip:5551234567@domain.com -> 5551234567
//   - tel:+15551234567 -> +15551234567
func ExtractPhoneFromURI(uri string) string {
	// Remove sip: or tel: prefix
	uri = strings.TrimPrefix(uri, "sip:")
	uri = strings.TrimPrefix(uri, "tel:")

	// Extract user part (before @)
	if idx := strings.Index(uri, "@"); idx != -1 {
		uri = uri[:idx]
	}

	// Remove any parameters (after ;)
	if idx := strings.Index(uri, ";"); idx != -1 {
		uri = uri[:idx]
	}

	// Clean up - keep only digits and + sign
	re := regexp.MustCompile(`[^\d+]`)
	phone := re.ReplaceAllString(uri, "")

	return phone
}

// FormatSIPURI formats a phone number as a SIP URI
func FormatSIPURI(phone, domain string) string {
	if domain == "" {
		domain = "localhost"
	}
	return fmt.Sprintf("sip:%s@%s", phone, domain)
}
