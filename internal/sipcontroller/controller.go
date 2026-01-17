package sipcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dense-identity/denseid/internal/subscriber"
	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

// Controller orchestrates Baresip and DIA protocol integration
type Controller struct {
	config    *Config
	diaConfig *dia.Config
	baresip   *BaresipClient
	callMgr   *CallManager
	peerCache *peerSessionCache
	// If cache was requested but failed to initialize (e.g., Redis unreachable),
	// Start() will fail with this error so cache-mode commands don't silently
	// fall back to AKE+RUA.
	peerCacheErr error

	ctx    context.Context
	cancel context.CancelFunc

	results chan CallResult

	registeredOnce sync.Once
	registeredCh   chan struct{}
}

// CallResult is emitted when a call reaches a terminal measurement state.
// For experiments, we primarily emit when CALL_ANSWERED is observed.
type CallResult struct {
	AttemptID        string `json:"attempt_id"`
	CallID           string `json:"call_id"`
	SelfPhone        string `json:"self_phone"`
	PeerPhone        string `json:"peer_phone"`
	PeerURI          string `json:"peer_uri"`
	Direction        string `json:"direction"`
	ProtocolEnabled  bool   `json:"protocol_enabled"`
	DialSentAtUnixMs int64  `json:"dial_sent_unix_ms"`
	AnsweredAtUnixMs int64  `json:"answered_unix_ms,omitempty"`
	LatencyMs        int64  `json:"latency_ms,omitempty"`
	ODAReqUnixMs     int64  `json:"oda_requested_unix_ms,omitempty"`
	ODADoneUnixMs    int64  `json:"oda_completed_unix_ms,omitempty"`
	ODALatencyMs     int64  `json:"oda_latency_ms,omitempty"`
	Outcome          string `json:"outcome"` // answered | closed | error
	Error            string `json:"error,omitempty"`
}

// NewController creates a new SIP controller
func NewController(cfg *Config, diaCfg *dia.Config) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	peerCache, peerCacheErr := newPeerSessionCache(cfg)
	if cfg != nil && cfg.CacheEnabled {
		if peerCacheErr != nil {
			log.Printf("[Cache] init failed: %v", peerCacheErr)
		} else if peerCache != nil && peerCache.enabled {
			log.Printf("[Cache] enabled redis=%s db=%d prefix=%s ttl=%s", cfg.RedisAddr, cfg.RedisDB, peerCache.prefix, peerCache.ttl)
		}
	}
	return &Controller{
		config:       cfg,
		diaConfig:    diaCfg,
		baresip:      NewBaresipClient(cfg.BaresipAddr, cfg.Verbose),
		callMgr:      NewCallManager(),
		peerCache:    peerCache,
		peerCacheErr: peerCacheErr,
		ctx:          ctx,
		cancel:       cancel,
		results:      make(chan CallResult, 1000),
		registeredCh: make(chan struct{}),
	}
}

func (c *Controller) markRegistered() {
	c.registeredOnce.Do(func() {
		close(c.registeredCh)
	})
}

// WaitForRegister blocks until the controller observes a REGISTER_OK event.
func (c *Controller) WaitForRegister(ctx context.Context, timeout time.Duration) error {
	// Fast-path if already registered
	select {
	case <-c.registeredCh:
		return nil
	default:
	}

	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.registeredCh:
		return nil
	case <-t.C:
		return fmt.Errorf("timeout waiting for REGISTER_OK")
	}
}

// Results returns a stream of per-call results for experiments.
func (c *Controller) Results() <-chan CallResult {
	return c.results
}

// HangupCall requests Baresip to end a call by call-id.
// Best-effort helper used by experiment mode to avoid accumulating open calls.
func (c *Controller) HangupCall(callID string) error {
	if c == nil || c.baresip == nil {
		return fmt.Errorf("controller not started")
	}
	if strings.TrimSpace(callID) == "" {
		return fmt.Errorf("missing callID")
	}
	_, err := c.baresip.Hangup(callID, 0, "")
	return err
}

// Start connects to Baresip and starts the event loop
func (c *Controller) Start() error {
	if c.config != nil && c.config.CacheEnabled {
		if c.peerCacheErr != nil {
			return fmt.Errorf("peer-session cache init failed: %w", c.peerCacheErr)
		}
		if c.peerCache == nil || !c.peerCache.enabled {
			return fmt.Errorf("peer-session cache is enabled but not available")
		}
	}

	// Connect to Baresip
	if err := c.baresip.Connect(); err != nil {
		return fmt.Errorf("connecting to baresip: %w", err)
	}

	// Best-effort registration snapshot. This avoids guessing based on periodic REGISTER_OK.
	// Gate behind verbose to avoid frequent large log lines during experiments.
	if c.config.Verbose {
		if resp, err := c.baresip.RegInfo(); err == nil {
			log.Printf("[Baresip] reginfo ok=%v data=%q", resp.OK, resp.Data)
		} else {
			log.Printf("[Baresip] reginfo failed: %v", err)
		}
	}

	// Start event processing
	go c.eventLoop()

	log.Printf("[Controller] Started, listening for Baresip events")
	return nil
}

// Stop gracefully shuts down the controller
func (c *Controller) Stop() {
	log.Printf("[Controller] Shutting down...")
	c.cancel()

	// Cleanup all sessions
	for _, session := range c.callMgr.GetAllSessions() {
		session.Cleanup()
	}

	c.baresip.Close()
	if c.peerCache != nil {
		c.peerCache.Close()
	}
}

func (c *Controller) cacheEnabled() bool {
	return c != nil && c.config != nil && c.config.CacheEnabled && c.peerCache != nil && c.peerCache.enabled
}

func (c *Controller) cacheGetPeerSession(selfPhone, peerPhone string) ([]byte, bool) {
	if !c.cacheEnabled() {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(c.ctx, 500*time.Millisecond)
	defer cancel()
	b, ok, err := c.peerCache.Get(ctx, selfPhone, peerPhone)
	if err != nil {
		log.Printf("[Cache] get failed self=%s peer=%s: %v", selfPhone, peerPhone, err)
		return nil, false
	}
	if ok {
		log.Printf("[Cache] HIT self=%s peer=%s bytes=%d", selfPhone, peerPhone, len(b))
	} else {
		log.Printf("[Cache] MISS self=%s peer=%s", selfPhone, peerPhone)
	}
	return b, ok
}

func (c *Controller) cacheStorePeerSession(selfPhone, peerPhone string, blob []byte) {
	if !c.cacheEnabled() {
		return
	}
	if len(blob) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 500*time.Millisecond)
	defer cancel()
	if err := c.peerCache.Set(ctx, selfPhone, peerPhone, blob); err != nil {
		log.Printf("[Cache] set failed self=%s peer=%s: %v", selfPhone, peerPhone, err)
		return
	}
	log.Printf("[Cache] STORED self=%s peer=%s bytes=%d", selfPhone, peerPhone, len(blob))
}

// eventLoop processes Baresip events
func (c *Controller) eventLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return

		case event, ok := <-c.baresip.Events():
			if !ok {
				return
			}
			c.handleBaresipEvent(event)

		case err, ok := <-c.baresip.Errors():
			if !ok {
				return
			}
			log.Printf("[Controller] Baresip error: %v", err)
		}
	}
}

// handleBaresipEvent routes Baresip events to appropriate handlers
func (c *Controller) handleBaresipEvent(event BaresipEvent) {
	// Log events sparingly by default to avoid perturbing measurements and overwhelming logs.
	logEvent := c.config.Verbose
	if !logEvent {
		switch event.Type {
		case EventCallIncoming, EventCallOutgoing, EventCallRinging, EventCallAnswered, EventCallEstablished, EventCallClosed,
			EventRegisterOK, EventRegisterFail:
			logEvent = true
		default:
			logEvent = false
		}
	}
	if logEvent {
		// Avoid printing large SDP bodies in logs unless verbose.
		if (event.Type == EventCallLocalSDP || event.Type == EventCallRemoteSDP) && !c.config.Verbose {
			// Skip SDP events entirely when not verbose.
		} else if event.Type == EventCallLocalSDP || event.Type == EventCallRemoteSDP {
			log.Printf("[Controller] Event: type=%s class=%s dir=%s aor=%s id=%s peer=%s",
				event.Type, event.Class, event.Direction, event.AccountAOR, event.ID, event.PeerURI)
		} else {
			log.Printf("[Controller] Event: type=%s class=%s dir=%s aor=%s id=%s peer=%s param=%q",
				event.Type, event.Class, event.Direction, event.AccountAOR, event.ID, event.PeerURI, event.Param)
		}
	}

	switch event.Type {
	case EventCallIncoming:
		c.handleIncomingCall(event)

	case EventCallRemoteSDP:
		// Some baresip setups don't emit CALL_INCOMING; the callee may only see
		// CALL_REMOTE_SDP (class=other) before any CALL_* (class=call) events.
		// If we don't have a session yet, treat this as the incoming-call signal.
		if c.callMgr.GetByCallID(event.ID) == nil {
			c.handleIncomingCall(event)
		}

	case EventCallOutgoing:
		c.handleCallOutgoing(event)

	case EventCallRinging:
		c.handleCallRinging(event)

	case EventCallAnswered:
		c.handleCallAnswered(event)

	case EventCallEstablished:
		c.handleCallEstablished(event)

	case EventCallClosed:
		c.handleCallClosed(event)

	case EventCallProgress:
		// Call is progressing, may receive early media
		log.Printf("[Controller] Call %s: progress", event.ID)

	case EventRegisterOK:
		c.markRegistered()
		if event.AccountAOR != "" {
			log.Printf("[Controller] REGISTER_OK: %s", event.AccountAOR)
		} else {
			log.Printf("[Controller] REGISTER_OK")
		}

	case EventRegisterFail:
		if event.AccountAOR != "" {
			log.Printf("[Controller] REGISTER_FAIL: %s (%s)", event.AccountAOR, event.Param)
		} else {
			log.Printf("[Controller] REGISTER_FAIL: %s", event.Param)
		}

	default:
		if c.config.Verbose {
			log.Printf("[Controller] Unhandled event: %s", event.Type)
		}
	}
}

// InitiateOutgoingCall starts an outgoing call. If protocolEnabled is true, DIA is started in parallel.
// Returns the controller-generated attempt ID.
func (c *Controller) InitiateOutgoingCall(phoneNumber string, protocolEnabled bool) (string, error) {
	log.Printf("[Controller] Initiating outgoing call to %s (protocol=%v)", phoneNumber, protocolEnabled)

	// Best-effort registration snapshot before placing an outgoing call.
	// Gate behind verbose to avoid large/frequent logs during experiments.
	if c.config.Verbose {
		if resp, err := c.baresip.RegInfo(); err == nil {
			log.Printf("[Baresip] reginfo ok=%v data=%q", resp.OK, resp.Data)
		} else {
			log.Printf("[Baresip] reginfo failed: %v", err)
		}
	}

	peerPhone := ExtractPhoneFromURI(phoneNumber)
	if existing := c.callMgr.GetActiveSessionByPeer(peerPhone); existing != nil {
		err := fmt.Errorf("peer %s already has an active session (attempt=%s call-id=%s state=%s)",
			peerPhone, existing.AttemptID, existing.CallID, existing.GetState().String())
		// Emit an error record so experiments remain correlated even when we refuse
		// to start a second overlapping call.
		session := c.callMgr.NewOutgoingAttempt(peerPhone, "", protocolEnabled)
		c.emitResult(CallResult{
			AttemptID:       session.AttemptID,
			CallID:          session.CallID,
			SelfPhone:       c.config.SelfPhone,
			PeerPhone:       session.PeerPhone,
			PeerURI:         session.PeerURI,
			Direction:       session.Direction,
			ProtocolEnabled: session.ProtocolEnabled,
			Outcome:         "error",
			Error:           err.Error(),
		})
		session.SetState(StateClosed)
		session.Cleanup()
		c.callMgr.RemoveSession(session)
		return session.AttemptID, err
	}

	// Create outgoing attempt (call-id not known yet)
	session := c.callMgr.NewOutgoingAttempt(peerPhone, "", protocolEnabled)
	session.SetState(StateDIAInitializing)

	// Start DIA protocol in background (only when enabled)
	if protocolEnabled {
		go c.runOutgoingDIA(session)
	}

	// Dial via Baresip
	resp, sentAt, err := c.baresip.DialWithSentAt(phoneNumber)
	if resp != nil {
		log.Printf("[Baresip] dial ok=%v data=%q", resp.OK, resp.Data)
	}
	session.DialSentAt = sentAt
	// Non-blocking diagnostics: if we never observe a call-id event, capture
	// listcalls snapshots shortly after dial. This helps distinguish:
	// - ctrl_tcp dial accepted but baresip did not create a call
	// - call created but events not arriving / delayed
	if c.config.Verbose {
		go func(attemptID string) {
			snapshot := func(delay time.Duration) {
				t := time.NewTimer(delay)
				defer t.Stop()
				select {
				case <-c.ctx.Done():
					return
				case <-t.C:
				}
				s := c.callMgr.GetByAttemptID(attemptID)
				if s == nil {
					return
				}
				state := s.GetState()
				if state == StateClosed || s.CallID != "" {
					return
				}
				log.Printf("[Controller] No call-id yet for attempt=%s peer=%s state=%s after=%s", attemptID, s.PeerPhone, state.String(), delay)
				if lr, lerr := c.baresip.ListCalls(); lerr == nil {
					log.Printf("[Baresip] listcalls ok=%v data=%q", lr.OK, lr.Data)
				} else {
					log.Printf("[Baresip] listcalls failed: %v", lerr)
				}
			}
			snapshot(300 * time.Millisecond)
			snapshot(1500 * time.Millisecond)
		}(session.AttemptID)
	}

	// If the call already reached ANSWERED/ESTABLISHED before the dial command response
	// was processed, we may already have AnsweredAt. Emit the answered metric now.
	c.maybeEmitAnsweredResult(session)
	if err != nil {
		session.LastError = err
		c.emitResult(CallResult{
			AttemptID:        session.AttemptID,
			CallID:           session.CallID,
			SelfPhone:        c.config.SelfPhone,
			PeerPhone:        session.PeerPhone,
			PeerURI:          session.PeerURI,
			Direction:        session.Direction,
			ProtocolEnabled:  session.ProtocolEnabled,
			DialSentAtUnixMs: sentAt.UnixMilli(),
			Outcome:          "error",
			Error:            err.Error(),
		})
		session.Cleanup()
		c.callMgr.RemoveSession(session)
		return "", fmt.Errorf("baresip dial failed: %w", err)
	}

	if !resp.OK {
		err := fmt.Errorf("baresip dial rejected: %s", resp.Data)
		session.LastError = err
		c.emitResult(CallResult{
			AttemptID:        session.AttemptID,
			CallID:           session.CallID,
			SelfPhone:        c.config.SelfPhone,
			PeerPhone:        session.PeerPhone,
			PeerURI:          session.PeerURI,
			Direction:        session.Direction,
			ProtocolEnabled:  session.ProtocolEnabled,
			DialSentAtUnixMs: sentAt.UnixMilli(),
			Outcome:          "error",
			Error:            err.Error(),
		})
		session.Cleanup()
		c.callMgr.RemoveSession(session)
		return "", err
	}

	session.SetState(StateBaresipDialing)
	log.Printf("[Controller] Dial sent (attempt=%s) for %s: %s", session.AttemptID, phoneNumber, resp.Data)

	return session.AttemptID, nil
}

func (c *Controller) maybeEmitAnsweredResult(session *CallSession) {
	if session == nil {
		return
	}
	if !session.IsOutgoing() {
		return
	}
	if session.AnsweredResultEmitted {
		return
	}
	if session.DialSentAt.IsZero() || session.AnsweredAt.IsZero() {
		return
	}

	lat := session.AnsweredAt.Sub(session.DialSentAt).Milliseconds()
	c.emitResult(CallResult{
		AttemptID:        session.AttemptID,
		CallID:           session.CallID,
		SelfPhone:        c.config.SelfPhone,
		PeerPhone:        session.PeerPhone,
		PeerURI:          session.PeerURI,
		Direction:        session.Direction,
		ProtocolEnabled:  session.ProtocolEnabled,
		DialSentAtUnixMs: session.DialSentAt.UnixMilli(),
		AnsweredAtUnixMs: session.AnsweredAt.UnixMilli(),
		LatencyMs:        lat,
		Outcome:          "answered",
	})
	session.AnsweredResultEmitted = true
}

func (c *Controller) emitResult(r CallResult) {
	select {
	case c.results <- r:
	default:
		// Avoid blocking controller; drop if overwhelmed.
	}

	// Also log JSON for easy automation.
	// In experiments we typically print a single record per attempt from main,
	// so avoid duplicating noisy close records when the call was already answered.
	if r.Outcome == "closed" && !c.config.Verbose && r.AnsweredAtUnixMs != 0 {
		return
	}
	if data, err := json.Marshal(r); err == nil {
		log.Printf("[Result] %s", string(data))
	}
}

// runOutgoingDIA runs the DIA protocol for an outgoing call
func (c *Controller) runOutgoingDIA(session *CallSession) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Controller] DIA panic for %s: %v", session.PeerPhone, r)
		}
	}()

	// Create DIA call state (outgoing = true for caller)
	diaState, err := dia.NewCallState(c.diaConfig, session.PeerPhone, true)
	if err != nil {
		log.Printf("[Controller] Failed to create DIA state for %s: %v", session.PeerPhone, err)
		session.LastError = err
		return
	}
	session.DIAState = diaState

	// Initialize AKE
	if err := diaState.AKEInit(); err != nil {
		log.Printf("[Controller] AKE init failed for %s: %v", session.PeerPhone, err)
		session.LastError = err
		return
	}

	// Cache lookup and apply (best-effort). On cache hit we skip sending AKE request
	// and go straight to RUA-only. On cache miss we continue with AKE+RUA.
	var cacheHit bool
	if blob, ok := c.cacheGetPeerSession(c.config.SelfPhone, session.PeerPhone); ok {
		if err := diaState.ApplyPeerSession(blob); err != nil {
			log.Printf("[Cache] apply failed self=%s peer=%s: %v (treating as miss)", c.config.SelfPhone, session.PeerPhone, err)
			cacheHit = false
		} else {
			cacheHit = true
			log.Printf("[Cache] applied peer-session; running RUA-only for %s", session.PeerPhone)
		}
	}

	session.SetState(StateDIAAKEInProgress)

	// Get AKE topic and create DIA controller
	akeTopic, _ := diaState.AKETopic()
	log.Printf("[Controller] DIA AKE topic for %s: %s", session.PeerPhone, akeTopic)

	diaController, err := subscriber.NewController(diaState, &subscriber.ControllerConfig{
		RelayServerAddr: c.config.RelayAddr,
		UseTLS:          c.config.RelayTLS,
	})
	if err != nil {
		log.Printf("[Controller] Failed to create DIA controller for %s: %v", session.PeerPhone, err)
		session.LastError = err
		return
	}
	session.DIAController = diaController

	// Start DIA controller with message handler
	diaController.Start(func(data []byte) {
		c.handleDIAMessage(session, data)
	})

	if cacheHit {
		// Derive RUA topic
		ruaTopic, err := diaState.RUADeriveTopic()
		if err != nil {
			log.Printf("[Controller] RUA topic derivation failed (cache-hit) for %s: %v", session.PeerPhone, err)
			session.LastError = err
			return
		}
		// Create RUA request
		ruaRequest, err := diaState.RUARequest()
		if err != nil {
			log.Printf("[Controller] RUA request failed (cache-hit) for %s: %v", session.PeerPhone, err)
			session.LastError = err
			return
		}
		// Ticket for topic creation
		ticket, err := diaState.Ticket()
		if err != nil {
			log.Printf("[Controller] Ticket derivation failed (cache-hit) for %s: %v", session.PeerPhone, err)
			session.LastError = err
			return
		}
		// Transition to RUA
		if err := diaState.TransitionToRUA(); err != nil {
			log.Printf("[Controller] Transition to RUA failed (cache-hit) for %s: %v", session.PeerPhone, err)
			session.LastError = err
			return
		}
		session.SetState(StateDIARUAInProgress)
		if err := diaController.SubscribeToNewTopicWithPayload(ruaTopic, ruaRequest, ticket); err != nil {
			log.Printf("[Controller] Subscribe to RUA topic failed (cache-hit) for %s: %v", session.PeerPhone, err)
			session.LastError = err
			return
		}
		log.Printf("[Controller] Cache-hit: subscribed to RUA and sent RUA request for %s", session.PeerPhone)
		return
	}

	// Send AKE request (caller initiates)
	request, err := diaState.AKERequest()
	if err != nil {
		log.Printf("[Controller] Failed to create AKE request for %s: %v", session.PeerPhone, err)
		session.LastError = err
		return
	}

	if err := diaController.Send(request); err != nil {
		log.Printf("[Controller] Failed to send AKE request for %s: %v", session.PeerPhone, err)
		session.LastError = err
		return
	}

	log.Printf("[Controller] Sent AKE request for %s", session.PeerPhone)
}

// handleIncomingCall handles an incoming call event from Baresip
func (c *Controller) handleIncomingCall(event BaresipEvent) {
	peerPhone := ExtractPhoneFromURI(event.PeerURI)
	log.Printf("[Controller] Incoming call from %s (call-id: %s)", peerPhone, event.ID)

	// Integrated mode uses a deterministic DIA topic per peer. Running multiple
	// concurrent sessions against the same peer can cause duplicate protocol
	// responses and AKE finalize errors. If we already have an active session
	// for this peer, reject as Busy.
	if c.config.IncomingMode != "baseline" {
		if existing := c.callMgr.GetActiveSessionByPeer(peerPhone); existing != nil {
			log.Printf("[Controller] Rejecting incoming call %s from %s: active session exists (attempt=%s call-id=%s state=%s)",
				event.ID, peerPhone, existing.AttemptID, existing.CallID, existing.GetState().String())
			if event.ID != "" {
				_, _ = c.baresip.Hangup(event.ID, 486, "Busy Here")
			}
			return
		}
	}

	// Create session
	session := c.callMgr.NewIncomingSession(event.ID, event.PeerURI, event.PeerName, event.AccountAOR)

	// Baseline mode: always auto-answer, no DIA.
	if c.config.IncomingMode == "baseline" {
		session.ProtocolEnabled = false
		session.SetState(StateBaresipRinging)
		c.answerCall(session)
		return
	}

	// Integrated mode: run DIA gating, then answer after verification.
	session.ProtocolEnabled = true

	session.SetState(StateDIAInitializing)

	// Set timeout for DIA protocol
	session.DIATimeout = time.AfterFunc(time.Duration(c.config.TimeoutSec)*time.Second, func() {
		c.handleDIATimeout(session)
	})

	// Run DIA as recipient
	go c.runIncomingDIA(session)
}

// handleCallOutgoing binds an outgoing call-id to the next pending attempt for the peer.
func (c *Controller) handleCallOutgoing(event BaresipEvent) {
	peerPhone := ExtractPhoneFromURI(event.PeerURI)
	session := c.callMgr.GetByCallID(event.ID)
	if session == nil {
		session = c.callMgr.BindOutgoingCallID(peerPhone, event.ID, event.PeerURI, event.AccountAOR)
	}
	if session == nil {
		log.Printf("[Controller] CALL_OUTGOING for %s (peer=%s) with no pending attempt", event.ID, peerPhone)
		return
	}
	log.Printf("[Controller] Outgoing call bound: attempt=%s call-id=%s peer=%s", session.AttemptID, session.CallID, peerPhone)
}

// runIncomingDIA runs the DIA protocol for an incoming call
func (c *Controller) runIncomingDIA(session *CallSession) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Controller] DIA panic for incoming %s: %v", session.PeerPhone, r)
			c.rejectCall(session, 500, "Internal Error")
		}
	}()

	// Create DIA call state (outgoing = false for recipient)
	diaState, err := dia.NewCallState(c.diaConfig, session.PeerPhone, false)
	if err != nil {
		log.Printf("[Controller] Failed to create DIA state for incoming %s: %v", session.PeerPhone, err)
		c.rejectCall(session, 500, "DIA Init Failed")
		return
	}
	session.DIAState = diaState

	// Initialize AKE
	if err := diaState.AKEInit(); err != nil {
		log.Printf("[Controller] AKE init failed for incoming %s: %v", session.PeerPhone, err)
		c.rejectCall(session, 500, "AKE Init Failed")
		return
	}

	// Cache lookup and apply (best-effort). On cache hit we skip AKE entirely and
	// subscribe to RUA immediately (RUA-only). On cache miss we wait for AKE request.
	var cacheHit bool
	if blob, ok := c.cacheGetPeerSession(c.config.SelfPhone, session.PeerPhone); ok {
		if err := diaState.ApplyPeerSession(blob); err != nil {
			log.Printf("[Cache] apply failed self=%s peer=%s: %v (treating as miss)", c.config.SelfPhone, session.PeerPhone, err)
			cacheHit = false
		} else {
			cacheHit = true
			log.Printf("[Cache] applied peer-session; running RUA-only for incoming %s", session.PeerPhone)
		}
	}

	session.SetState(StateDIAAKEInProgress)

	// Get AKE topic and create DIA controller
	akeTopic, _ := diaState.AKETopic()
	log.Printf("[Controller] DIA AKE topic for incoming %s: %s", session.PeerPhone, akeTopic)

	diaController, err := subscriber.NewController(diaState, &subscriber.ControllerConfig{
		RelayServerAddr: c.config.RelayAddr,
		UseTLS:          c.config.RelayTLS,
	})
	if err != nil {
		log.Printf("[Controller] Failed to create DIA controller for incoming %s: %v", session.PeerPhone, err)
		c.rejectCall(session, 500, "DIA Controller Failed")
		return
	}
	session.DIAController = diaController

	// Start DIA controller with message handler
	diaController.Start(func(data []byte) {
		c.handleDIAMessage(session, data)
	})

	if cacheHit {
		// Derive RUA topic
		ruaTopic, err := diaState.RUADeriveTopic()
		if err != nil {
			log.Printf("[Controller] RUA topic derivation failed (cache-hit) for incoming %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Topic Failed")
			return
		}
		// Transition to RUA
		if err := diaState.TransitionToRUA(); err != nil {
			log.Printf("[Controller] Transition to RUA failed (cache-hit) for incoming %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Transition Failed")
			return
		}
		// Subscribe to RUA topic (ticketed)
		ticket, err := diaState.Ticket()
		if err != nil {
			log.Printf("[Controller] Ticket derivation failed (cache-hit) for incoming %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Ticket Failed")
			return
		}
		if err := diaController.SubscribeToNewTopicWithPayload(ruaTopic, nil, ticket); err != nil {
			log.Printf("[Controller] Subscribe to RUA topic failed (cache-hit) for incoming %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Topic Subscribe Failed")
			return
		}
		// Initialize RUA responder
		if err := diaState.RUAInit(); err != nil {
			log.Printf("[Controller] RUA init failed (cache-hit) for incoming %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Init Failed")
			return
		}
		session.SetState(StateDIARUAInProgress)
		log.Printf("[Controller] Cache-hit: moved to RUA, waiting for RUA request from %s", session.PeerPhone)
		return
	}

	log.Printf("[Controller] Waiting for AKE request from %s", session.PeerPhone)
}

// handleDIAMessage handles incoming DIA protocol messages
func (c *Controller) handleDIAMessage(session *CallSession, data []byte) {
	if session == nil {
		return
	}
	// Drop late/stray DIA messages after the SIP call has ended. This prevents
	// closed sessions from responding on the shared topic.
	if session.GetState() == StateClosed {
		return
	}
	if session.DIAState == nil || session.DIAController == nil {
		return
	}

	if len(data) == 0 {
		log.Printf("[Controller] Ignoring empty DIA payload")
		return
	}

	msg, err := dia.ParseMessage(data)
	if err != nil {
		log.Printf("[Controller] Failed to parse DIA message: %v", err)
		return
	}

	msgSenderID, _ := msg.SenderID()
	mySenderID, _ := session.DIAState.SenderID()

	// Ignore self-authored messages
	if msgSenderID == mySenderID {
		return
	}

	log.Printf("[Controller] DIA message type=%d for %s", msg.Type(), session.PeerPhone)

	// Handle BYE message
	if msg.Type() == dia.MsgBye {
		log.Printf("[Controller] Received DIA BYE for %s", session.PeerPhone)
		return
	}

	// Route based on role and message type
	if session.IsOutgoing() {
		c.handleCallerDIAMessage(session, msg, data)
	} else {
		c.handleRecipientDIAMessage(session, msg, data)
	}
}

// handleCallerDIAMessage handles DIA messages for the caller
func (c *Controller) handleCallerDIAMessage(session *CallSession, msg *dia.Message, rawData []byte) {
	switch msg.Type() {
	case dia.MsgAKEResponse:
		log.Printf("[Controller] Handling AKE Response for outgoing %s", session.PeerPhone)

		oldTopic, _ := session.DIAState.CurrentTopic()

		complete, err := session.DIAState.AKEComplete(rawData)
		if err != nil {
			log.Printf("[Controller] AKE complete failed for %s: %v", session.PeerPhone, err)
			session.LastError = err
			return
		}

		// Send AKE complete ASAP on the AKE topic so the recipient can finalize.
		// Topic switching is still required for security; we just avoid putting it
		// in front of the message that unblocks the other side.
		if err := session.DIAController.SendToTopic(oldTopic, complete, nil); err != nil {
			log.Printf("[Controller] Send AKE complete failed for %s: %v", session.PeerPhone, err)
			return
		}

		// Derive RUA topic
		ruaTopic, err := session.DIAState.RUADeriveTopic()
		if err != nil {
			log.Printf("[Controller] RUA topic derivation failed for %s: %v", session.PeerPhone, err)
			return
		}

		// Create RUA request
		ruaRequest, err := session.DIAState.RUARequest()
		if err != nil {
			log.Printf("[Controller] RUA request failed for %s: %v", session.PeerPhone, err)
			return
		}

		// Get ticket for topic creation
		ticket, _ := session.DIAState.Ticket()

		// Transition to RUA
		if err := session.DIAState.TransitionToRUA(); err != nil {
			log.Printf("[Controller] Transition to RUA failed for %s: %v", session.PeerPhone, err)
			return
		}

		session.SetState(StateDIARUAInProgress)

		// Subscribe to RUA and piggy-back the first RUA request in the same request.
		if err := session.DIAController.SubscribeToNewTopicWithPayload(ruaTopic, ruaRequest, ticket); err != nil {
			log.Printf("[Controller] Subscribe to RUA topic failed for %s: %v", session.PeerPhone, err)
			return
		}

		log.Printf("[Controller] AKE complete, subscribed to RUA for %s", session.PeerPhone)

	case dia.MsgRUAResponse:
		log.Printf("[Controller] Handling RUA Response for outgoing %s", session.PeerPhone)

		if err := session.DIAState.RUAFinalize(rawData); err != nil {
			log.Printf("[Controller] RUA finalize failed for %s: %v", session.PeerPhone, err)
			session.LastError = err
			return
		}

		session.DIAComplete = true
		session.SetState(StateDIAComplete)

		// Caller-side optional ODA measurement: trigger ODA after the call is answered.
		// RUA may complete before SIP answer; whichever happens last should start ODA.
		c.startODAAfterAnswerForOutgoingIfConfigured(session)

		// Get remote party info
		remoteParty, err := session.DIAState.RemoteParty()
		if err == nil && remoteParty != nil {
			session.RemoteParty = remoteParty
			log.Printf("[Controller] Remote party for %s: name=%s verified=%v logo=%s",
				session.PeerPhone, remoteParty.Name, remoteParty.Verified, remoteParty.Logo)
			if remoteParty.Verified {
				if blob, err := session.DIAState.ExportPeerSession(); err != nil {
					log.Printf("[Cache] export failed self=%s peer=%s: %v", c.config.SelfPhone, session.PeerPhone, err)
				} else {
					c.cacheStorePeerSession(c.config.SelfPhone, session.PeerPhone, blob)
				}
			}
		}

		// ODA for incoming calls is triggered after answer in integrated incoming mode only.

	case dia.MsgODARequest:
		c.handleODARequest(session, rawData)

	case dia.MsgODAResponse:
		c.handleODAResponse(session, rawData)
	}
}

// handleRecipientDIAMessage handles DIA messages for the recipient
func (c *Controller) handleRecipientDIAMessage(session *CallSession, msg *dia.Message, rawData []byte) {
	switch msg.Type() {
	case dia.MsgAKERequest:
		log.Printf("[Controller] Handling AKE Request for incoming %s", session.PeerPhone)

		response, err := session.DIAState.AKEResponse(rawData)
		if err != nil {
			log.Printf("[Controller] AKE response failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "AKE Failed")
			return
		}

		if err := session.DIAController.Send(response); err != nil {
			log.Printf("[Controller] Send AKE response failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "AKE Send Failed")
			return
		}

		log.Printf("[Controller] Sent AKE Response for %s", session.PeerPhone)

	case dia.MsgAKEComplete:
		log.Printf("[Controller] Handling AKE Complete for incoming %s", session.PeerPhone)

		if err := session.DIAState.AKEFinalize(rawData); err != nil {
			log.Printf("[Controller] AKE finalize failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "AKE Finalize Failed")
			return
		}

		// Derive RUA topic
		ruaTopic, err := session.DIAState.RUADeriveTopic()
		if err != nil {
			log.Printf("[Controller] RUA topic derivation failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Topic Failed")
			return
		}

		// Transition to RUA
		if err := session.DIAState.TransitionToRUA(); err != nil {
			log.Printf("[Controller] Transition to RUA failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Transition Failed")
			return
		}

		// Recipient subscribes to the RUA topic (ticketed) so it can receive the RUA request.
		ticket, err := session.DIAState.Ticket()
		if err != nil {
			log.Printf("[Controller] Ticket derivation failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Ticket Failed")
			return
		}
		if err := session.DIAController.SubscribeToNewTopicWithPayload(ruaTopic, nil, ticket); err != nil {
			log.Printf("[Controller] Subscribe to RUA topic failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Topic Subscribe Failed")
			return
		}

		// Initialize RUA
		if err := session.DIAState.RUAInit(); err != nil {
			log.Printf("[Controller] RUA init failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Init Failed")
			return
		}

		session.SetState(StateDIARUAInProgress)
		log.Printf("[Controller] Moved to RUA, waiting for RUA request from %s", session.PeerPhone)

	case dia.MsgRUARequest:
		log.Printf("[Controller] Handling RUA Request for incoming %s", session.PeerPhone)

		response, err := session.DIAState.RUAResponse(rawData)
		if err != nil {
			log.Printf("[Controller] RUA response failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Failed")
			return
		}

		if err := session.DIAController.Send(response); err != nil {
			log.Printf("[Controller] Send RUA response failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Send Failed")
			return
		}

		session.DIAComplete = true
		session.SetState(StateDIAComplete)

		// Get remote party info
		remoteParty, err := session.DIAState.RemoteParty()
		if err == nil && remoteParty != nil {
			session.RemoteParty = remoteParty
			log.Printf("[Controller] Remote party for %s: name=%s verified=%v logo=%s",
				session.PeerPhone, remoteParty.Name, remoteParty.Verified, remoteParty.Logo)
			if remoteParty.Verified {
				if blob, err := session.DIAState.ExportPeerSession(); err != nil {
					log.Printf("[Cache] export failed self=%s peer=%s: %v", c.config.SelfPhone, session.PeerPhone, err)
				} else {
					c.cacheStorePeerSession(c.config.SelfPhone, session.PeerPhone, blob)
				}
			}
		}

		// Cancel timeout and answer the call
		session.CancelTimeout()

		// GATE DECISION: Answer the call if verification passed
		if remoteParty != nil && remoteParty.Verified {
			c.answerCall(session)
		} else {
			log.Printf("[Controller] Caller %s not verified, rejecting", session.PeerPhone)
			c.rejectCall(session, 603, "Not Verified")
		}

	case dia.MsgODARequest:
		c.handleODARequest(session, rawData)

	case dia.MsgODAResponse:
		c.handleODAResponse(session, rawData)
	}
}

// handleODARequest handles incoming ODA request
func (c *Controller) handleODARequest(session *CallSession, rawData []byte) {
	log.Printf("[Controller] Handling ODA Request for %s", session.PeerPhone)

	response, err := session.DIAState.ODAResponse(rawData)
	if err != nil {
		log.Printf("[Controller] ODA response failed for %s: %v", session.PeerPhone, err)
		return
	}

	if err := session.DIAController.Send(response); err != nil {
		log.Printf("[Controller] Send ODA response failed for %s: %v", session.PeerPhone, err)
		return
	}

	log.Printf("[Controller] Sent ODA Response for %s", session.PeerPhone)
}

// handleODAResponse handles incoming ODA response
func (c *Controller) handleODAResponse(session *CallSession, rawData []byte) {
	log.Printf("[Controller] Handling ODA Response for %s", session.PeerPhone)

	verification, err := session.DIAState.ODAVerify(rawData)
	if err != nil {
		log.Printf("[Controller] ODA verify failed for %s: %v", session.PeerPhone, err)
		return
	}

	log.Printf("[Controller] ODA Verification for %s: verified=%v issuer=%s",
		session.PeerPhone, verification.Verified, verification.Issuer)

	for name, value := range verification.DisclosedAttributes {
		log.Printf("[Controller]   %s: %s", name, value)
	}

	// If this controller initiated ODA, record terminal timing and (later) emit a result row.
	session.mu.Lock()
	shouldRecord := !session.ODARequestedAt.IsZero() && session.ODACompletedAt.IsZero()
	if shouldRecord {
		now := time.Now()
		session.ODACompletedAt = now
		session.ODATerminalOutcome = "oda_done"
		session.ODATerminalError = ""
	}
	session.mu.Unlock()
	if !shouldRecord {
		return
	}

	session.CancelODATimeout()
	c.maybeEmitODATerminalResult(session)

	if session.CallID != "" {
		_, err := c.baresip.Hangup(session.CallID, 0, "")
		if err != nil {
			log.Printf("[Controller] Hangup after ODA failed for %s: %v", session.CallID, err)
		}
	} else {
		// ODA may complete before the SIP call-id is known on the caller.
		// Hang up once we learn the call-id (e.g., on CALL_ANSWERED/ESTABLISHED).
		session.mu.Lock()
		session.HangupAfterODARequested = true
		session.mu.Unlock()
	}
}

func (c *Controller) maybeEmitODATerminalResult(session *CallSession) {
	if session == nil {
		return
	}

	session.mu.Lock()
	if session.ODATerminalEmitted || session.ODATerminalOutcome == "" {
		session.mu.Unlock()
		return
	}
	if session.ODARequestedAt.IsZero() || session.ODACompletedAt.IsZero() {
		session.mu.Unlock()
		return
	}
	// Require call-id for correlation.
	if session.CallID == "" {
		session.mu.Unlock()
		return
	}

	// Snapshot fields while holding the lock.
	res := CallResult{
		AttemptID:       session.AttemptID,
		CallID:          session.CallID,
		SelfPhone:       c.config.SelfPhone,
		PeerPhone:       session.PeerPhone,
		PeerURI:         session.PeerURI,
		Direction:       session.Direction,
		ProtocolEnabled: session.ProtocolEnabled,
		ODAReqUnixMs:    session.ODARequestedAt.UnixMilli(),
		ODADoneUnixMs:   session.ODACompletedAt.UnixMilli(),
		ODALatencyMs:    session.ODACompletedAt.Sub(session.ODARequestedAt).Milliseconds(),
		Outcome:         session.ODATerminalOutcome,
		Error:           session.ODATerminalError,
	}
	if !session.DialSentAt.IsZero() {
		res.DialSentAtUnixMs = session.DialSentAt.UnixMilli()
	}
	if !session.AnsweredAt.IsZero() {
		res.AnsweredAtUnixMs = session.AnsweredAt.UnixMilli()
		if !session.DialSentAt.IsZero() {
			res.LatencyMs = session.AnsweredAt.Sub(session.DialSentAt).Milliseconds()
		}
	}

	session.ODATerminalEmitted = true
	session.mu.Unlock()

	c.emitResult(res)
}

func (c *Controller) startODAAfterAnswerIfConfigured(session *CallSession) {
	if c.config.IncomingMode != "integrated" {
		return
	}
	if !session.IsIncoming() {
		return
	}
	if len(c.config.ODAAttributes) == 0 {
		return
	}
	if session.DIAState == nil || session.DIAController == nil || !session.DIAComplete {
		log.Printf("[Controller] Cannot start ODA after answer: DIA not complete for %s", session.PeerPhone)
		return
	}
	if !session.ODARequestedAt.IsZero() {
		return
	}

	session.ODARequestedAt = time.Now()
	log.Printf("[Controller] Starting ODA after answer for %s", session.PeerPhone)

	// Start timeout guard
	if c.config.ODATimeoutSec > 0 {
		session.ODATimeout = time.AfterFunc(time.Duration(c.config.ODATimeoutSec)*time.Second, func() {
			session.mu.Lock()
			shouldRecord := !session.ODARequestedAt.IsZero() && session.ODACompletedAt.IsZero()
			if shouldRecord {
				now := time.Now()
				session.ODACompletedAt = now
				session.ODATerminalOutcome = "oda_timeout"
				session.ODATerminalError = "oda timeout"
			}
			callID := session.CallID
			callIDKnown := callID != ""
			if shouldRecord && !callIDKnown {
				session.HangupAfterODARequested = true
			}
			session.mu.Unlock()
			if !shouldRecord {
				return
			}

			c.maybeEmitODATerminalResult(session)
			if callIDKnown {
				_, _ = c.baresip.Hangup(callID, 0, "")
			}
		})
	}

	go c.triggerODA(session, c.config.ODAAttributes)
}

func (c *Controller) startODAAfterAnswerForOutgoingIfConfigured(session *CallSession) {
	if session == nil {
		return
	}
	if session.IsIncoming() {
		return
	}
	if c.config.OutgoingODADelaySec < 0 {
		return
	}
	if len(c.config.ODAAttributes) == 0 {
		return
	}
	if !session.ProtocolEnabled {
		return
	}
	if !session.DIAComplete {
		return
	}
	// Only trigger after the call is answered/established. RUA may complete earlier.
	if session.AnsweredAt.IsZero() {
		return
	}
	// Avoid double-scheduling if we receive repeated events.
	session.mu.Lock()
	if session.ODARequestScheduled || !session.ODARequestedAt.IsZero() {
		session.mu.Unlock()
		return
	}
	session.ODARequestScheduled = true
	session.mu.Unlock()

	go func() {
		defer func() {
			// If we never actually sent an ODA request, allow a retry.
			if session.ODARequestedAt.IsZero() {
				session.mu.Lock()
				session.ODARequestScheduled = false
				session.mu.Unlock()
			}
		}()

		if c.config.OutgoingODADelaySec > 0 {
			t := time.NewTimer(time.Duration(c.config.OutgoingODADelaySec) * time.Second)
			defer t.Stop()
			select {
			case <-c.ctx.Done():
				return
			case <-t.C:
			}
		}

		now := time.Now()
		session.mu.Lock()
		session.ODARequestedAt = now
		session.mu.Unlock()
		log.Printf("[Controller] Starting outgoing ODA after answer for %s", session.PeerPhone)

		// Start timeout guard
		if c.config.ODATimeoutSec > 0 {
			session.ODATimeout = time.AfterFunc(time.Duration(c.config.ODATimeoutSec)*time.Second, func() {
				session.mu.Lock()
				shouldRecord := !session.ODARequestedAt.IsZero() && session.ODACompletedAt.IsZero()
				if shouldRecord {
					now := time.Now()
					session.ODACompletedAt = now
					session.ODATerminalOutcome = "oda_timeout"
					session.ODATerminalError = "oda timeout"
				}
				callID := session.CallID
				callIDKnown := callID != ""
				if shouldRecord && !callIDKnown {
					session.HangupAfterODARequested = true
				}
				session.mu.Unlock()
				if !shouldRecord {
					return
				}

				c.maybeEmitODATerminalResult(session)
				if callIDKnown {
					_, _ = c.baresip.Hangup(callID, 0, "")
				}
			})
		}

		c.triggerODA(session, c.config.ODAAttributes)
	}()
}

// triggerODA initiates an ODA request
func (c *Controller) triggerODA(session *CallSession, attributes []string) {
	if session.DIAState == nil || session.DIAController == nil {
		log.Printf("[Controller] Cannot trigger ODA: DIA not ready for %s", session.PeerPhone)
		return
	}

	log.Printf("[Controller] Triggering ODA for %s with attributes: %v", session.PeerPhone, attributes)

	request, err := session.DIAState.ODARequest(attributes)
	if err != nil {
		log.Printf("[Controller] ODA request creation failed for %s: %v", session.PeerPhone, err)
		return
	}

	if err := session.DIAController.Send(request); err != nil {
		log.Printf("[Controller] Send ODA request failed for %s: %v", session.PeerPhone, err)
		return
	}

	session.SetState(StateODAActive)
	log.Printf("[Controller] Sent ODA Request for %s", session.PeerPhone)
}

// handleDIATimeout handles DIA protocol timeout for incoming calls
func (c *Controller) handleDIATimeout(session *CallSession) {
	log.Printf("[Controller] DIA timeout for %s", session.PeerPhone)
	c.rejectCall(session, 603, "Authentication Timeout")
}

// answerCall sends accept command to Baresip
func (c *Controller) answerCall(session *CallSession) {
	log.Printf("[Controller] Answering call %s from %s", session.CallID, session.PeerPhone)

	resp, err := c.baresip.Accept(session.CallID)
	if err != nil {
		log.Printf("[Controller] Accept failed for %s: %v", session.CallID, err)
		return
	}

	if !resp.OK {
		log.Printf("[Controller] Accept rejected for %s: %s", session.CallID, resp.Data)
		return
	}

	log.Printf("[Controller] Call answered: %s", session.CallID)
}

// rejectCall sends hangup command to Baresip
func (c *Controller) rejectCall(session *CallSession, scode int, reason string) {
	log.Printf("[Controller] Rejecting call %s from %s: %d %s",
		session.CallID, session.PeerPhone, scode, reason)

	session.CancelTimeout()

	if session.CallID != "" {
		_, err := c.baresip.Hangup(session.CallID, scode, reason)
		if err != nil {
			log.Printf("[Controller] Hangup failed for %s: %v", session.CallID, err)
		}
	}

	session.SetState(StateClosed)
	session.Cleanup()
	c.callMgr.RemoveSession(session)
}

// handleCallRinging handles call ringing event
func (c *Controller) handleCallRinging(event BaresipEvent) {
	session := c.callMgr.GetByCallID(event.ID)
	if session == nil {
		// On the callee, some baresip setups emit CALL_RINGING (direction=incoming)
		// without a prior CALL_INCOMING. In baseline mode we still want to auto-answer.
		if event.Direction == "incoming" {
			c.handleIncomingCall(event)
			return
		}
		peerPhone := ExtractPhoneFromURI(event.PeerURI)
		session = c.callMgr.BindOutgoingCallID(peerPhone, event.ID, event.PeerURI, event.AccountAOR)
	}
	if session == nil {
		return
	}
	session.SetState(StateBaresipRinging)
	log.Printf("[Controller] Call %s ringing", event.ID)
}

// handleCallAnswered captures the primary experiment metric: caller-side latency to CALL_ANSWERED.
func (c *Controller) handleCallAnswered(event BaresipEvent) {
	session := c.callMgr.GetByCallID(event.ID)
	if session == nil {
		peerPhone := ExtractPhoneFromURI(event.PeerURI)
		session = c.callMgr.BindOutgoingCallID(peerPhone, event.ID, event.PeerURI, event.AccountAOR)
	}
	if session == nil {
		log.Printf("[Controller] CALL_ANSWERED for %s with no session", event.ID)
		return
	}

	// Some baresip instances emit CALL_ESTABLISHED but not CALL_ANSWERED; others emit both.
	// If we've already captured AnsweredAt, don't overwrite it.
	if session.AnsweredAt.IsZero() {
		session.AnsweredAt = time.Now()
	}
	// Emit answered metric if we have both timestamps.
	c.maybeEmitAnsweredResult(session)

	// Caller-side ODA measurement: trigger after answer (but requires RUA completion too).
	c.startODAAfterAnswerForOutgoingIfConfigured(session)

	// Recipient-side ODA measurement: start ODA after CALL_ANSWERED in integrated mode.
	c.startODAAfterAnswerIfConfigured(session)

	// If ODA completed earlier (e.g., caller-side) and was waiting for call-id, emit it now.
	c.maybeEmitODATerminalResult(session)

	// If ODA already completed before we had a call-id (caller-side), hang up now.
	session.mu.Lock()
	shouldHangup := session.HangupAfterODARequested && session.CallID != ""
	if shouldHangup {
		session.HangupAfterODARequested = false
	}
	session.mu.Unlock()
	if shouldHangup {
		_, err := c.baresip.Hangup(session.CallID, 0, "")
		if err != nil {
			log.Printf("[Controller] Hangup-after-ODA failed for %s: %v", session.CallID, err)
		}
	}
}

// handleCallEstablished handles call established event
func (c *Controller) handleCallEstablished(event BaresipEvent) {
	session := c.callMgr.GetByCallID(event.ID)
	if session == nil {
		peerPhone := ExtractPhoneFromURI(event.PeerURI)
		session = c.callMgr.BindOutgoingCallID(peerPhone, event.ID, event.PeerURI, event.AccountAOR)
	}

	if session != nil {
		session.SIPEstablished = true
		session.SetState(StateBaresipEstablished)
		log.Printf("[Controller] Call %s established", event.ID)

		// Some baresip setups do not emit CALL_ANSWERED. Treat CALL_ESTABLISHED as the
		// answer signal (but keep first AnsweredAt if already set).
		if session.AnsweredAt.IsZero() {
			session.AnsweredAt = time.Now()
		}
		// Emit answered metric if we have both timestamps.
		c.maybeEmitAnsweredResult(session)
		// Caller-side ODA measurement: also allow triggering on ESTABLISHED when ANSWERED isn't emitted.
		c.startODAAfterAnswerForOutgoingIfConfigured(session)
		// Recipient-side ODA measurement: also allow ODA-after-answer on ESTABLISHED.
		c.startODAAfterAnswerIfConfigured(session)
		// Outgoing ODA is triggered after SIP answer/established (and requires RUA completion).

		// If ODA completed earlier and was waiting for call-id, emit it now.
		c.maybeEmitODATerminalResult(session)

		// If ODA already completed before we had a call-id (caller-side), hang up now.
		session.mu.Lock()
		shouldHangup := session.HangupAfterODARequested && session.CallID != ""
		if shouldHangup {
			session.HangupAfterODARequested = false
		}
		session.mu.Unlock()
		if shouldHangup {
			_, err := c.baresip.Hangup(session.CallID, 0, "")
			if err != nil {
				log.Printf("[Controller] Hangup-after-ODA failed for %s: %v", session.CallID, err)
			}
		}

		// Display remote party info if available
		if session.RemoteParty != nil {
			if session.IsIncoming() {
				log.Printf("[Controller] ===== VERIFIED CALLER =====")
			} else {
				log.Printf("[Controller] ===== VERIFIED CALLEE =====")
			}
			log.Printf("[Controller]   Name: %s", session.RemoteParty.Name)
			log.Printf("[Controller]   Phone: %s", session.RemoteParty.Phone)
			log.Printf("[Controller]   Verified: %v", session.RemoteParty.Verified)
			if session.RemoteParty.Logo != "" {
				log.Printf("[Controller]   Logo: %s", session.RemoteParty.Logo)
			}
			log.Printf("[Controller] ============================")
		}

		// Integrated incoming mode triggers ODA after answer.

		// Baseline incoming flow: answer then end the call.
		if session.IsIncoming() && c.config.IncomingMode == "baseline" {
			c.scheduleBaselineHangup(session)
		}
	}
}

func (c *Controller) scheduleBaselineHangup(session *CallSession) {
	if session == nil || session.CallID == "" {
		return
	}

	session.mu.Lock()
	if session.BaselineHangupScheduled {
		session.mu.Unlock()
		return
	}
	session.BaselineHangupScheduled = true
	session.mu.Unlock()

	go func(callID string) {
		// Small delay so the caller reliably observes establishment first.
		time.Sleep(250 * time.Millisecond)
		_, err := c.baresip.Hangup(callID, 0, "")
		if err != nil {
			log.Printf("[Controller] Baseline hangup failed for %s: %v", callID, err)
			return
		}
		log.Printf("[Controller] Baseline hangup sent for %s", callID)
	}(session.CallID)
}

// handleCallClosed handles call closed event
func (c *Controller) handleCallClosed(event BaresipEvent) {
	session := c.callMgr.GetByCallID(event.ID)
	if session == nil {
		peerPhone := ExtractPhoneFromURI(event.PeerURI)
		session = c.callMgr.BindOutgoingCallID(peerPhone, event.ID, event.PeerURI, event.AccountAOR)
		if session == nil {
			log.Printf("[Controller] Call %s closed (no session)", event.ID)
			return
		}
	}

	// This often contains low-level socket text (e.g., ECONNRESET from media) even
	// for normal teardown. Only surface the reason by default if the call closed
	// before we observed it as answered/established.
	if session.AnsweredAt.IsZero() {
		log.Printf("[Controller] Call %s closed before answer: %s", event.ID, event.Param)
	} else if c.config.Verbose {
		log.Printf("[Controller] Call %s closed: %s", event.ID, event.Param)
	} else {
		log.Printf("[Controller] Call %s closed", event.ID)
	}

	// Send DIA BYE message if DIA is active
	if session.DIAController != nil && session.DIAState != nil {
		bye, err := session.DIAState.CreateByeMessage()
		if err != nil {
			log.Printf("[Controller] Failed to create DIA BYE for %s: %v", session.PeerPhone, err)
		} else {
			log.Printf("[Controller] Sending DIA BYE for %s", session.PeerPhone)
			if err := session.DIAController.Send(bye); err != nil {
				log.Printf("[Controller] Failed to send DIA BYE for %s: %v", session.PeerPhone, err)
			}
		}
	}

	session.SetState(StateClosed)
	// Always emit a close event for experiments so callers can wait for the call to
	// actually tear down before starting the next attempt.
	res := CallResult{
		AttemptID:       session.AttemptID,
		CallID:          session.CallID,
		SelfPhone:       c.config.SelfPhone,
		PeerPhone:       session.PeerPhone,
		PeerURI:         session.PeerURI,
		Direction:       session.Direction,
		ProtocolEnabled: session.ProtocolEnabled,
		Outcome:         "closed",
		Error:           event.Param,
	}
	if !session.DialSentAt.IsZero() {
		res.DialSentAtUnixMs = session.DialSentAt.UnixMilli()
	}
	if !session.AnsweredAt.IsZero() {
		res.AnsweredAtUnixMs = session.AnsweredAt.UnixMilli()
		if !session.DialSentAt.IsZero() {
			res.LatencyMs = session.AnsweredAt.Sub(session.DialSentAt).Milliseconds()
		}
	}
	c.emitResult(res)

	session.Cleanup()
	c.callMgr.RemoveSession(session)
}

// TriggerODAForCall allows external triggering of ODA for a call
func (c *Controller) TriggerODAForCall(callID string, attributes []string) error {
	session := c.callMgr.GetByCallID(callID)
	if session == nil {
		return fmt.Errorf("call not found: %s", callID)
	}

	if !session.DIAComplete {
		return fmt.Errorf("DIA not complete for call: %s", callID)
	}

	go c.triggerODA(session, attributes)
	return nil
}

// ListActiveCalls returns information about active calls
func (c *Controller) ListActiveCalls() []map[string]interface{} {
	sessions := c.callMgr.GetAllSessions()
	result := make([]map[string]interface{}, 0, len(sessions))

	for _, s := range sessions {
		info := map[string]interface{}{
			"call_id":    s.CallID,
			"peer_phone": s.PeerPhone,
			"direction":  s.Direction,
			"state":      s.GetState().String(),
			"dia_done":   s.DIAComplete,
			"sip_estab":  s.SIPEstablished,
		}
		if s.RemoteParty != nil {
			info["remote_name"] = s.RemoteParty.Name
			info["verified"] = s.RemoteParty.Verified
		}
		result = append(result, info)
	}

	return result
}
