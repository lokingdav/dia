package sipcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

	ctx    context.Context
	cancel context.CancelFunc

	results chan CallResult
}

// CallResult is emitted when a call reaches a terminal measurement state.
// For experiments, we primarily emit when CALL_ANSWERED is observed.
type CallResult struct {
	AttemptID        string `json:"attempt_id"`
	CallID           string `json:"call_id"`
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
	return &Controller{
		config:    cfg,
		diaConfig: diaCfg,
		baresip:   NewBaresipClient(cfg.BaresipAddr, cfg.Verbose),
		callMgr:   NewCallManager(),
		ctx:       ctx,
		cancel:    cancel,
		results:   make(chan CallResult, 1000),
	}
}

// Results returns a stream of per-call results for experiments.
func (c *Controller) Results() <-chan CallResult {
	return c.results
}

// Start connects to Baresip and starts the event loop
func (c *Controller) Start() error {
	// Connect to Baresip
	if err := c.baresip.Connect(); err != nil {
		return fmt.Errorf("connecting to baresip: %w", err)
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
	log.Printf("[Controller] Event: type=%s class=%s id=%s peer=%s",
		event.Type, event.Class, event.ID, event.PeerURI)

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

	peerPhone := ExtractPhoneFromURI(phoneNumber)

	// Create outgoing attempt (call-id not known yet)
	session := c.callMgr.NewOutgoingAttempt(peerPhone, "", protocolEnabled)
	session.SetState(StateDIAInitializing)

	// Start DIA protocol in background (only when enabled)
	if protocolEnabled {
		go c.runOutgoingDIA(session)
	}

	// Dial via Baresip
	resp, sentAt, err := c.baresip.DialWithSentAt(phoneNumber)
	session.DialSentAt = sentAt
	// If the call already reached ANSWERED/ESTABLISHED before the dial command response
	// was processed, we may already have AnsweredAt. Emit the answered metric now.
	c.maybeEmitAnsweredResult(session)
	if err != nil {
		session.LastError = err
		c.emitResult(CallResult{
			AttemptID:        session.AttemptID,
			CallID:           session.CallID,
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

	log.Printf("[Controller] Waiting for AKE request from %s", session.PeerPhone)
}

// handleDIAMessage handles incoming DIA protocol messages
func (c *Controller) handleDIAMessage(session *CallSession, data []byte) {
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

		// Transition to RUA
		if err := session.DIAState.TransitionToRUA(); err != nil {
			log.Printf("[Controller] Transition to RUA failed for %s: %v", session.PeerPhone, err)
			return
		}

		session.SetState(StateDIARUAInProgress)

		// Get ticket for topic creation
		ticket, _ := session.DIAState.Ticket()

		// Subscribe to RUA topic
		if err := session.DIAController.SubscribeToNewTopicWithPayload(ruaTopic, ruaRequest, ticket); err != nil {
			log.Printf("[Controller] Subscribe to RUA topic failed for %s: %v", session.PeerPhone, err)
			return
		}

		// Send AKE complete on old topic
		if err := session.DIAController.SendToTopic(oldTopic, complete, nil); err != nil {
			log.Printf("[Controller] Send AKE complete failed for %s: %v", session.PeerPhone, err)
			return
		}

		log.Printf("[Controller] AKE complete, moved to RUA for %s", session.PeerPhone)

	case dia.MsgRUAResponse:
		log.Printf("[Controller] Handling RUA Response for outgoing %s", session.PeerPhone)

		if err := session.DIAState.RUAFinalize(rawData); err != nil {
			log.Printf("[Controller] RUA finalize failed for %s: %v", session.PeerPhone, err)
			session.LastError = err
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
		}

		// Auto-trigger ODA if configured
		if c.config.AutoODA && len(c.config.ODAAttributes) > 0 {
			go c.triggerODA(session, c.config.ODAAttributes)
		}

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

		// Swap to RUA topic
		if err := session.DIAController.SwapToTopic(ruaTopic, nil, nil); err != nil {
			log.Printf("[Controller] Swap to RUA topic failed for %s: %v", session.PeerPhone, err)
			c.rejectCall(session, 500, "RUA Topic Swap Failed")
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

	// If this controller initiated ODA (integrated incoming flow), record latency and hang up.
	if !session.ODARequestedAt.IsZero() && session.ODACompletedAt.IsZero() {
		now := time.Now()
		session.ODACompletedAt = now
		session.CancelODATimeout()

		odaLat := now.Sub(session.ODARequestedAt).Milliseconds()
		c.emitResult(CallResult{
			AttemptID:       session.AttemptID,
			CallID:          session.CallID,
			PeerPhone:       session.PeerPhone,
			PeerURI:         session.PeerURI,
			Direction:       session.Direction,
			ProtocolEnabled: session.ProtocolEnabled,
			ODAReqUnixMs:    session.ODARequestedAt.UnixMilli(),
			ODADoneUnixMs:   now.UnixMilli(),
			ODALatencyMs:    odaLat,
			Outcome:         "oda_done",
		})

		if session.CallID != "" {
			_, err := c.baresip.Hangup(session.CallID, 0, "")
			if err != nil {
				log.Printf("[Controller] Hangup after ODA failed for %s: %v", session.CallID, err)
			}
		}
	}
}

func (c *Controller) startODAAfterAnswerIfConfigured(session *CallSession) {
	if c.config.IncomingMode != "integrated" {
		return
	}
	if !c.config.ODAAfterAnswer {
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
			if !session.ODARequestedAt.IsZero() && session.ODACompletedAt.IsZero() {
				now := time.Now()
				c.emitResult(CallResult{
					AttemptID:       session.AttemptID,
					CallID:          session.CallID,
					PeerPhone:       session.PeerPhone,
					PeerURI:         session.PeerURI,
					Direction:       session.Direction,
					ProtocolEnabled: session.ProtocolEnabled,
					ODAReqUnixMs:    session.ODARequestedAt.UnixMilli(),
					ODADoneUnixMs:   now.UnixMilli(),
					ODALatencyMs:    now.Sub(session.ODARequestedAt).Milliseconds(),
					Outcome:         "oda_timeout",
					Error:           "oda timeout",
				})
				if session.CallID != "" {
					_, _ = c.baresip.Hangup(session.CallID, 0, "")
				}
			}
		})
	}

	go c.triggerODA(session, c.config.ODAAttributes)
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

	// Recipient-side ODA measurement: start ODA after CALL_ANSWERED in integrated mode.
	c.startODAAfterAnswerIfConfigured(session)
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
		// Recipient-side ODA measurement: also allow ODA-after-answer on ESTABLISHED.
		c.startODAAfterAnswerIfConfigured(session)

		// Display remote party info if available
		if session.RemoteParty != nil {
			log.Printf("[Controller] ===== VERIFIED CALLER =====")
			log.Printf("[Controller]   Name: %s", session.RemoteParty.Name)
			log.Printf("[Controller]   Phone: %s", session.RemoteParty.Phone)
			log.Printf("[Controller]   Verified: %v", session.RemoteParty.Verified)
			if session.RemoteParty.Logo != "" {
				log.Printf("[Controller]   Logo: %s", session.RemoteParty.Logo)
			}
			log.Printf("[Controller] ============================")
		}

		// Auto-trigger ODA for incoming calls if configured
		if session.IsIncoming() && c.config.AutoODA && len(c.config.ODAAttributes) > 0 {
			go c.triggerODA(session, c.config.ODAAttributes)
		}

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
		log.Printf("[Controller] Call %s closed (no session)", event.ID)
		return
	}

	log.Printf("[Controller] Call %s closed: %s", event.ID, event.Param)

	// Send DIA BYE message if DIA is active
	if session.DIAController != nil && session.DIAState != nil {
		// TODO: Send BYE message via DIA protocol
		log.Printf("[Controller] Sending DIA BYE for %s", session.PeerPhone)
	}

	session.SetState(StateClosed)
	// Always emit a close event for experiments so callers can wait for the call to
	// actually tear down before starting the next attempt.
	res := CallResult{
		AttemptID:       session.AttemptID,
		CallID:          session.CallID,
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
