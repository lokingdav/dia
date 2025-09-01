package subscriber

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	relaypb "github.com/dense-identity/denseid/api/go/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Session holds one subscription (topic, ticket, senderID) over a Client.
type Session struct {
	client   *RelayClient
	topic    string
	ticket   []byte
	senderID string

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
	s.wg.Add(1)
	go s.recvLoop()
}

// Send publishes one payload with the configured senderID. Returns error on failure.
// Uses ticket if available, otherwise publishes to existing topic without ticket.
func (s *Session) Send(payload []byte) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}
	// Short per-call deadline for snappy UX
	ctx, cancel := context.WithTimeout(s.ctx, s.publishTimeout)
	defer cancel()

	_, err := s.client.Stub.Publish(ctx, &relaypb.PublishRequest{
		Topic:  s.topic,
		Ticket: s.ticket, // Use ticket if available for topic creation
		Message: &relaypb.RelayMessage{
			Topic:    s.topic,
			Payload:  payload,
			SenderId: s.senderID,
		},
	})
	if err != nil {
		// For app logic, you might want to surface NotFound (topic missing) distinctly.
		return err
	}
	return nil
}

// Close stops receiving and waits for cleanup.
func (s *Session) Close() {
	if s.closed.Swap(true) {
		return
	}
	s.cancel()
	s.wg.Wait()
}

func (s *Session) recvLoop() {
	defer s.wg.Done()

	backoffIdx := 0
	for {
		if s.ctx.Err() != nil {
			return
		}

		// Start/Restart the Subscribe stream
		stream, err := s.client.Stub.Subscribe(s.ctx, &relaypb.SubscribeRequest{
			Topic:    s.topic,
			SenderId: s.senderID,
		})
		if err != nil {
			// Transient failures: back off and retry
			if s.transient(err) && s.sleepBackoff(backoffIdx) {
				backoffIdx++
				continue
			}
			// Permanent or context canceled: exit
			return
		}

		// Successful connect: reset backoff
		backoffIdx = 0

		// Read loop
		for {
			m, err := stream.Recv()
			if err == nil {
				if m != nil && s.onMessage != nil {
					// Deliver bytes immediately; app can decode
					s.onMessage(m.GetPayload())
				}
				continue
			}

			// Handle stream termination
			if err == io.EOF {
				// Server closed cleanly; attempt reconnect
			} else if st, ok := status.FromError(err); ok {
				// Context canceled? we're shutting down.
				if st.Code() == codes.Canceled || st.Code() == codes.DeadlineExceeded {
					return
				}
				// Otherwise, treat as transient and reconnect.
			} else {
				// Non-status error; treat as transient for POC.
			}

			// Reconnect with backoff unless we're closed
			if s.sleepBackoff(backoffIdx) {
				backoffIdx++
				break
			}
			return
		}
	}
}

func (s *Session) sleepBackoff(idx int) bool {
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

func (s *Session) transient(err error) bool {
	if s.ctx.Err() != nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return true // assume transient for non-status errors in POC
	}
	switch st.Code() {
	case codes.Unavailable, codes.ResourceExhausted, codes.Canceled, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}
