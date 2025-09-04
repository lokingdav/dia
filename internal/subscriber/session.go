package subscriber

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Session holds one subscription (topic, ticket, senderID) over a Client.
// This is a compatibility wrapper around the new StreamSession.
type Session struct {
	streamSession *StreamSession
	client        *RelayClient
	topic         string
	ticket        []byte
	senderID      string

	onMessage func([]byte)

	// lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool

	// dialing/recv behavior
	retryBackoff []time.Duration
	// outbound timeout for Publish
	publishTimeout time.Duration
}

// NewSession prepares a session. Call Start(...) to begin receiving.
func NewSession(client *RelayClient, topic string, ticket []byte, senderID string) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		client:         client,
		topic:          topic,
		ticket:         ticket,
		senderID:       senderID,
		ctx:            ctx,
		cancel:         cancel,
		retryBackoff:   []time.Duration{0, 500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 5 * time.Second},
		publishTimeout: 3 * time.Second,
	}
}

// Start begins the subscribe loop and sets the inbound callback.
func (s *Session) Start(onMessage func([]byte)) {
	if s.closed.Load() {
		return
	}
	s.onMessage = onMessage

	// Create and start the new streaming session
	s.streamSession = NewStreamSession(s.client, s.senderID)
	if err := s.streamSession.Start(onMessage); err != nil {
		return
	}

	// Subscribe to the initial topic if provided
	if s.topic != "" {
		s.streamSession.Subscribe(s.topic, s.ticket, true)
	}
}

// Send publishes one payload with the configured senderID. Returns error on failure.
// Uses ticket if available, otherwise publishes to existing topic without ticket.
func (s *Session) Send(payload []byte) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}

	if s.streamSession == nil {
		return errors.New("session not started")
	}

	return s.streamSession.Publish(payload)
}

// SendToTopic publishes one payload to a specific topic (different from the subscribed topic)
func (s *Session) SendToTopic(topic string, payload []byte, ticket []byte) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}

	if s.streamSession == nil {
		return errors.New("session not started")
	}

	return s.streamSession.PublishToTopic(topic, payload, ticket)
}

// SubscribeToNewTopic creates a new subscription to a different topic
func (s *Session) SubscribeToNewTopic(newTopic string) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}

	if s.streamSession == nil {
		return errors.New("session not started")
	}

	// Update our stored topic for future reference
	s.topic = newTopic

	// Use the new SwitchTopic functionality
	return s.streamSession.SwitchTopic(newTopic, s.ticket, true)
}

// Close stops receiving and waits for cleanup.
func (s *Session) Close() {
	if s.closed.Swap(true) {
		return
	}

	if s.streamSession != nil {
		s.streamSession.Close()
	}

	s.cancel()
	s.wg.Wait()
}
