package sipcontroller

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
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
	CallID    string // Baresip call-id
	PeerPhone string // E.164 phone number
	PeerURI   string // Full SIP URI
	PeerName  string // Display name if available
	Direction string // "outgoing" or "incoming"

	// State
	State          CallState
	DIAComplete    bool
	SIPEstablished bool

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

// Cleanup releases resources associated with the session
func (s *CallSession) Cleanup() {
	s.CancelTimeout()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.DIAController != nil {
		_ = s.DIAController.Close()
		s.DIAController = nil
	}
	if s.DIAState != nil {
		s.DIAState.Close()
		s.DIAState = nil
	}
}

// CallManager manages multiple concurrent call sessions
type CallManager struct {
	sessions map[string]*CallSession // callID -> session
	byPhone  map[string]*CallSession // phone -> session (for lookup before callID known)
	mu       sync.RWMutex
}

// NewCallManager creates a new call manager
func NewCallManager() *CallManager {
	return &CallManager{
		sessions: make(map[string]*CallSession),
		byPhone:  make(map[string]*CallSession),
	}
}

// CreateSession creates a new call session
func (m *CallManager) CreateSession(peerPhone, peerURI, direction string) *CallSession {
	session := NewCallSession(peerPhone, peerURI, direction)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Index by phone number initially (callID not yet known for outgoing)
	m.byPhone[peerPhone] = session

	return session
}

// RegisterCallID associates a callID with an existing session
func (m *CallManager) RegisterCallID(session *CallSession, callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session.CallID = callID
	m.sessions[callID] = session
}

// GetByCallID retrieves a session by call ID
func (m *CallManager) GetByCallID(callID string) *CallSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[callID]
}

// GetByPhone retrieves a session by phone number
func (m *CallManager) GetByPhone(phone string) *CallSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byPhone[phone]
}

// RemoveSession removes a session from the manager
func (m *CallManager) RemoveSession(session *CallSession) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session.CallID != "" {
		delete(m.sessions, session.CallID)
	}
	if session.PeerPhone != "" {
		delete(m.byPhone, session.PeerPhone)
	}
}

// GetAllSessions returns all active sessions
func (m *CallManager) GetAllSessions() []*CallSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*CallSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// Count returns the number of active sessions
func (m *CallManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
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
