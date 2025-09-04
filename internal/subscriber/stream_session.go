package subscriber

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	relaypb "github.com/dense-identity/denseid/api/go/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StreamSession represents a bidirectional streaming session with the relay server
type StreamSession struct {
	client   *RelayClient
	senderID string

	// Stream management
	stream      relaypb.RelayService_RelayClient
	streamMu    sync.RWMutex
	streamReady chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	closed      atomic.Bool

	// Message handling
	onMessage    func([]byte)
	retryBackoff []time.Duration

	// Current subscription state
	mu              sync.RWMutex
	currentTopic    string
	currentTicket   []byte
	subscribed      bool
	pendingRequests map[string]chan *relaypb.RelayResponse // For tracking request responses
}

// NewStreamSession creates a new bidirectional streaming session
func NewStreamSession(client *RelayClient, senderID string) *StreamSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &StreamSession{
		client:          client,
		senderID:        senderID,
		ctx:             ctx,
		cancel:          cancel,
		retryBackoff:    []time.Duration{0, 500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 5 * time.Second},
		pendingRequests: make(map[string]chan *relaypb.RelayResponse),
		streamReady:     make(chan struct{}),
	}
}

// Start establishes the bidirectional stream and begins processing
func (s *StreamSession) Start(onMessage func([]byte)) error {
	if s.closed.Load() {
		return errors.New("session is closed")
	}

	s.onMessage = onMessage
	s.wg.Add(1)
	go s.streamLoop()

	// Wait for stream to be ready - this is event-driven, not time-based
	select {
	case <-s.streamReady:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// Subscribe to a topic with optional history replay
func (s *StreamSession) Subscribe(topic string, ticket []byte, replayHistory bool) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}

	s.mu.Lock()
	s.currentTopic = topic
	s.currentTicket = ticket
	s.mu.Unlock()

	req := &relaypb.RelayRequest{
		Type:     relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_SUBSCRIBE,
		SenderId: s.senderID,
		Operation: &relaypb.RelayRequest_Subscribe{
			Subscribe: &relaypb.SubscribeOperation{
				Topic:         topic,
				ReplayHistory: replayHistory,
			},
		},
	}

	return s.sendRequest(req)
}

// Publish a message to the current topic
func (s *StreamSession) Publish(payload []byte) error {
	s.mu.RLock()
	topic := s.currentTopic
	ticket := s.currentTicket
	s.mu.RUnlock()

	if topic == "" {
		return errors.New("no topic subscribed")
	}

	return s.PublishToTopic(topic, payload, ticket)
}

// PublishToTopic publishes a message to a specific topic
func (s *StreamSession) PublishToTopic(topic string, payload []byte, ticket []byte) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}

	req := &relaypb.RelayRequest{
		Type:     relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_PUBLISH,
		SenderId: s.senderID,
		Operation: &relaypb.RelayRequest_Publish{
			Publish: &relaypb.PublishOperation{
				Topic:   topic,
				Ticket:  ticket,
				Payload: payload,
			},
		},
	}

	return s.sendRequest(req)
}

// SwitchTopic switches from current topic to a new topic (replaces subscription)
func (s *StreamSession) SwitchTopic(newTopic string, ticket []byte, replayHistory bool) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}

	s.mu.Lock()
	oldTopic := s.currentTopic
	s.currentTopic = newTopic
	s.currentTicket = ticket
	s.mu.Unlock()

	req := &relaypb.RelayRequest{
		Type:     relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_REPLACE,
		SenderId: s.senderID,
		Operation: &relaypb.RelayRequest_Replace{
			Replace: &relaypb.ReplaceOperation{
				OldTopic:      oldTopic,
				NewTopic:      newTopic,
				Ticket:        ticket,
				ReplayHistory: replayHistory,
			},
		},
	}

	return s.sendRequest(req)
}

// Unsubscribe from current topic
func (s *StreamSession) Unsubscribe() error {
	if s.closed.Load() {
		return errors.New("session closed")
	}

	s.mu.RLock()
	topic := s.currentTopic
	s.mu.RUnlock()

	if topic == "" {
		return nil // Already unsubscribed
	}

	req := &relaypb.RelayRequest{
		Type:     relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_UNSUBSCRIBE,
		SenderId: s.senderID,
		Operation: &relaypb.RelayRequest_Unsubscribe{
			Unsubscribe: &relaypb.UnsubscribeOperation{
				Topic: topic,
			},
		},
	}

	err := s.sendRequest(req)
	if err == nil {
		s.mu.Lock()
		s.currentTopic = ""
		s.currentTicket = nil
		s.subscribed = false
		s.mu.Unlock()
	}

	return err
}

// Close gracefully closes the session
func (s *StreamSession) Close() error {
	if s.closed.Swap(true) {
		return nil
	}

	// Send bye message
	req := &relaypb.RelayRequest{
		Type:     relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_BYE,
		SenderId: s.senderID,
		Operation: &relaypb.RelayRequest_Bye{
			Bye: &relaypb.ByeOperation{
				Reason: "Client closing",
			},
		},
	}
	s.sendRequest(req) // Best effort

	s.cancel()
	s.wg.Wait()
	return nil
}

// sendRequest sends a request over the stream
func (s *StreamSession) sendRequest(req *relaypb.RelayRequest) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}

	// Check if stream is ready - no timeout, fail fast if not ready
	select {
	case <-s.streamReady:
		// Stream is ready, proceed
	default:
		return errors.New("stream not ready")
	}

	s.streamMu.RLock()
	stream := s.stream
	s.streamMu.RUnlock()

	if stream == nil {
		return errors.New("stream not available")
	}

	// Send the request
	return stream.Send(req)
}

// streamLoop manages the bidirectional stream connection
func (s *StreamSession) streamLoop() {
	defer s.wg.Done()

	backoffIdx := 0
	streamReadySignaled := false

	for {
		if s.ctx.Err() != nil {
			return
		}

		// Establish stream
		stream, err := s.client.Stub.Relay(s.ctx)
		if err != nil {
			if s.transient(err) && s.sleepBackoff(backoffIdx) {
				backoffIdx++
				continue
			}
			log.Printf("Failed to establish relay stream: %v", err)
			return
		}

		// Set the stream safely
		s.streamMu.Lock()
		s.stream = stream
		s.streamMu.Unlock()

		// Signal that stream is ready (only once)
		if !streamReadySignaled {
			close(s.streamReady)
			streamReadySignaled = true
		}

		backoffIdx = 0 // Reset backoff on successful connection

		// Start send and receive loops
		sendCtx, sendCancel := context.WithCancel(s.ctx)
		recvCtx, recvCancel := context.WithCancel(s.ctx)

		var wg sync.WaitGroup
		wg.Add(2)

		// Receive loop
		go func() {
			defer wg.Done()
			defer recvCancel()
			s.receiveLoop(stream, recvCtx)
		}()

		// Send loop handles sending initial subscription if needed
		go func() {
			defer wg.Done()
			defer sendCancel()
			s.sendInitialSubscription(stream, sendCtx)
		}()

		// Wait for either loop to finish
		wg.Wait()

		// Clean up
		sendCancel()
		recvCancel()

		// Clear the stream safely
		s.streamMu.Lock()
		s.stream = nil
		s.streamMu.Unlock()

		// If context is cancelled, don't retry
		if s.ctx.Err() != nil {
			return
		}

		// Retry with backoff
		if s.sleepBackoff(backoffIdx) {
			backoffIdx++
		} else {
			return
		}
	}
} // receiveLoop handles incoming messages from the server
func (s *StreamSession) receiveLoop(stream relaypb.RelayService_RelayClient, ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				log.Printf("Server closed stream")
			} else {
				log.Printf("Error receiving from stream: %v", err)
			}
			return
		}

		s.handleResponse(resp)
	}
}

// sendInitialSubscription sends initial subscription if we have a current topic
func (s *StreamSession) sendInitialSubscription(stream relaypb.RelayService_RelayClient, ctx context.Context) {
	// Always send HELLO message first to register with server
	helloReq := &relaypb.RelayRequest{
		Type:     relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_HELLO,
		SenderId: s.senderID,
		Operation: &relaypb.RelayRequest_Hello{
			Hello: &relaypb.HelloOperation{},
		},
	}

	if err := stream.Send(helloReq); err != nil {
		log.Printf("Failed to send hello message: %v", err)
		return
	}

	// Check if we have a topic to subscribe to
	s.mu.RLock()
	topic := s.currentTopic
	s.mu.RUnlock()

	if topic != "" {
		// Send actual subscription
		req := &relaypb.RelayRequest{
			Type:     relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_SUBSCRIBE,
			SenderId: s.senderID,
			Operation: &relaypb.RelayRequest_Subscribe{
				Subscribe: &relaypb.SubscribeOperation{
					Topic:         topic,
					ReplayHistory: true,
				},
			},
		}

		if err := stream.Send(req); err != nil {
			log.Printf("Failed to send initial subscription: %v", err)
			return
		}

		s.mu.Lock()
		s.subscribed = true
		s.mu.Unlock()
	}

	// Keep this goroutine alive until context is cancelled
	// This becomes the send loop that handles future send operations
	<-ctx.Done()
}

// handleResponse processes incoming responses from the server
func (s *StreamSession) handleResponse(resp *relaypb.RelayResponse) {
	switch resp.GetType() {
	case relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_PUBLISH,
		relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_SUBSCRIBE:

		if msg := resp.GetMessage(); msg != nil {
			// This is a message delivery
			if s.onMessage != nil {
				s.onMessage(msg.GetPayload())
			}
		} else if ack := resp.GetAck(); ack != nil {
			// This is an operation acknowledgment
			log.Printf("Operation %v on topic %s: %s", ack.GetOperationType(), ack.GetTopic(), ack.GetMessage())
		}

	case relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_UNSUBSCRIBE,
		relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_REPLACE,
		relaypb.RelayMessageType_RELAY_MESSAGE_TYPE_BYE:

		if ack := resp.GetAck(); ack != nil {
			log.Printf("Operation %v: %s", ack.GetOperationType(), ack.GetMessage())
		}

	default:
		if errResp := resp.GetError(); errResp != nil {
			log.Printf("Error response for %v on topic %s: %s",
				errResp.GetOperationType(), errResp.GetTopic(), errResp.GetErrorMessage())
		}
	}
}

// sleepBackoff sleeps for the specified backoff duration
func (s *StreamSession) sleepBackoff(idx int) bool {
	if s.ctx.Err() != nil {
		return false
	}
	if idx >= len(s.retryBackoff) {
		idx = len(s.retryBackoff) - 1
	}
	select {
	case <-time.After(s.retryBackoff[idx]):
		return true
	case <-s.ctx.Done():
		return false
	}
}

// transient checks if an error is transient and should be retried
func (s *StreamSession) transient(err error) bool {
	if s.ctx.Err() != nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return true // assume transient for non-status errors
	}
	switch st.Code() {
	case codes.Unavailable, codes.ResourceExhausted, codes.Canceled, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}
