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
	dia "github.com/lokingdav/libdia/bindings/go/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Session holds one active topic (with ticket + senderID) over a single Tunnel stream.
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
	retryBackoff   []time.Duration
	publishTimeout time.Duration

	// tunnel state
	streamMu sync.RWMutex
	stream   relaypb.RelayService_TunnelClient

	// single-writer pump for outbound frames
	sendQ  chan *relaypb.RelayRequest
	sendWg sync.WaitGroup
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
		sendQ:          make(chan *relaypb.RelayRequest, 256),
	}
}

// SendImmediate attempts to send directly on the current stream (no queue), falling back
// to enqueue if the stream isn't established yet.
func (s *Session) SendImmediate(payload []byte) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}
	if len(payload) == 0 {
		return errors.New("empty payload")
	}
	ticket, err := s.buildMacTicket(relaypb.RelayRequest_PUBLISH, s.topic, payload, s.ticket)
	if err != nil {
		return err
	}
	req := &relaypb.RelayRequest{
		SenderId: s.senderID,
		Type:     relaypb.RelayRequest_PUBLISH,
		Topic:    s.topic,
		Payload:  payload,
		Ticket:   ticket,
	}

	s.streamMu.RLock()
	stream := s.stream
	s.streamMu.RUnlock()
	if stream != nil {
		return stream.Send(req)
	}
	return s.enqueue(req)
}

// Start begins the Tunnel loop and sets the inbound callback.
func (s *Session) Start(onMessage func([]byte)) {
	if s.closed.Load() {
		return
	}
	s.onMessage = onMessage
	s.wg.Add(1)
	go s.tunnelLoop()
}

// Send publishes one payload to the current topic.
// Uses the session's ticket only if topic creation is needed (server enforces).
func (s *Session) Send(payload []byte) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}
	if len(payload) == 0 {
		return errors.New("empty payload")
	}
	ticket, err := s.buildMacTicket(relaypb.RelayRequest_PUBLISH, s.topic, payload, s.ticket)
	if err != nil {
		return err
	}
	req := &relaypb.RelayRequest{
		SenderId: s.senderID,
		Type:     relaypb.RelayRequest_PUBLISH,
		Topic:    s.topic,
		Payload:  payload,
		Ticket:   ticket,
	}
	return s.enqueue(req)
}

// SendToTopic publishes payload to a specific topic.
// Note: PUBLISH does not consume a ticket; topics are created via SUBSCRIBE.
func (s *Session) SendToTopic(topic string, payload []byte, ticket []byte) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}
	if topic == "" || len(payload) == 0 {
		return errors.New("missing topic or payload")
	}
	macTicket, err := s.buildMacTicket(relaypb.RelayRequest_PUBLISH, topic, payload, ticket)
	if err != nil {
		return err
	}
	req := &relaypb.RelayRequest{
		SenderId: s.senderID,
		Type:     relaypb.RelayRequest_PUBLISH,
		Topic:    topic,
		Payload:  payload,
		Ticket:   macTicket,
	}
	return s.enqueue(req)
}

// SubscribeToNewTopicWithPayload subscribes to a new topic (with replay) and,
// if payload is non-nil/len>0, piggy-backs a one-shot publish to that topic.
// A ticket is only required if that publish would create the topic (server-enforced).
func (s *Session) SubscribeToNewTopicWithPayload(newTopic string, payload []byte, ticket []byte) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}
	if newTopic == "" {
		return errors.New("empty topic")
	}
	if len(ticket) == 0 {
		return errors.New("missing ticket")
	}
	macTicket, err := s.buildMacTicket(relaypb.RelayRequest_SUBSCRIBE, newTopic, payload, ticket)
	if err != nil {
		return err
	}
	// Optimistically set the intended active topic. If the server rejects the SUBSCRIBE
	// (e.g., missing/invalid ticket), it will send an ERROR and keep you on the
	// previous topic server-side; the client can choose to retry.
	s.topic = newTopic

	req := &relaypb.RelayRequest{
		SenderId: s.senderID,
		Type:     relaypb.RelayRequest_SUBSCRIBE,
		Topic:    newTopic,
		Payload:  payload,   // nil or empty => subscribe-only (no piggy-back)
		Ticket:   macTicket, // token_preimage || mac
	}
	return s.enqueue(req)
}

// Close stops the Tunnel and waits for cleanup.
func (s *Session) Close() {
	if s.closed.Swap(true) {
		return
	}
	s.cancel()
	// stop writer
	close(s.sendQ)
	s.sendWg.Wait()
	// stop reader/reconnector
	s.wg.Wait()
}

const (
	macTokenPreimageLen = 32
)

func (s *Session) macDataForReq(reqType relaypb.RelayRequest_Type, topic string, payload []byte) []byte {
	data := make([]byte, 0, 1+len(topic)+len(payload))
	data = append(data, byte(reqType))
	data = append(data, []byte(topic)...)
	data = append(data, payload...)
	return data
}

func (s *Session) buildMacTicket(reqType relaypb.RelayRequest_Type, topic string, payload []byte, token []byte) ([]byte, error) {
	if len(token) == 0 {
		token = s.ticket
	}
	if len(token) < macTokenPreimageLen {
		return nil, errors.New("ticket too short")
	}
	data := s.macDataForReq(reqType, topic, payload)
	mac, err := dia.CreateMessageMAC(token, data)
	if err != nil {
		return nil, err
	}
	preimage := token[:macTokenPreimageLen]
	macTicket := make([]byte, 0, macTokenPreimageLen+len(mac))
	macTicket = append(macTicket, preimage...)
	macTicket = append(macTicket, mac...)
	return macTicket, nil
}

// ===== internals =====

func (s *Session) tunnelLoop() {
	defer s.wg.Done()

	backoffIdx := 0
	for {
		if s.ctx.Err() != nil {
			return
		}

		// (Re)open Tunnel
		stream, err := s.client.Stub.Tunnel(s.ctx)
		if err != nil {
			log.Printf("relay Tunnel dial failed (addr=%s): %v", s.client.addr, err)
			if s.transient(err) && s.sleepBackoff(backoffIdx) {
				backoffIdx++
				continue
			}
			return
		}

		// Publish stream in session
		s.streamMu.Lock()
		s.stream = stream
		s.streamMu.Unlock()

		// Immediately SUBSCRIBE to current topic (with replay)
		macTicket, err := s.buildMacTicket(relaypb.RelayRequest_SUBSCRIBE, s.topic, nil, s.ticket)
		if err != nil {
			log.Printf("relay SUBSCRIBE build MAC failed (addr=%s topic=%s): %v", s.client.addr, s.topic, err)
			_ = stream.CloseSend()
			if s.transient(err) && s.sleepBackoff(backoffIdx) {
				backoffIdx++
				continue
			}
			return
		}
		sub := &relaypb.RelayRequest{
			SenderId: s.senderID,
			Type:     relaypb.RelayRequest_SUBSCRIBE,
			Topic:    s.topic,
			Ticket:   macTicket,
		}
		if err := stream.Send(sub); err != nil {
			log.Printf("relay SUBSCRIBE send failed (addr=%s topic=%s): %v", s.client.addr, s.topic, err)
			_ = stream.CloseSend()
			if s.transient(err) && s.sleepBackoff(backoffIdx) {
				backoffIdx++
				continue
			}
			return
		}
		log.Printf("relay Tunnel connected (addr=%s topic=%s)", s.client.addr, s.topic)

		// Start writer pump for queued frames
		sendCtx, sendCancel := context.WithCancel(s.ctx)
		s.sendWg.Add(1)
		go s.writerPump(sendCtx, stream)

		// Successful connect: reset backoff
		backoffIdx = 0

		// Read loop
		for {
			resp, recvErr := stream.Recv()
			if recvErr == nil {
				if resp == nil {
					continue
				}
				switch resp.GetType() {
				case relaypb.RelayResponse_EVENT:
					if s.onMessage != nil {
						s.onMessage(resp.GetPayload())
					}
				case relaypb.RelayResponse_ERROR:
					// Surface minimally via log; stream remains open.
					log.Printf("relay ERROR on topic %s: code=%d msg=%s", resp.GetTopic(), resp.GetCode(), resp.GetMessage())
				default:
					// ignore unknown
				}
				continue
			}

			// Stream terminated
			sendCancel() // stop writer
			_ = stream.CloseSend()

			if recvErr == io.EOF {
				// server closed cleanly; reconnect
			} else if st, ok := status.FromError(recvErr); ok {
				if st.Code() == codes.Canceled || st.Code() == codes.DeadlineExceeded {
					return
				}
				// transient -> loop and reconnect
			}
			// other errors: treat as transient for prototype

			if s.sleepBackoff(backoffIdx) {
				backoffIdx++
				break
			}
			return
		}
	}
}

func (s *Session) writerPump(ctx context.Context, stream relaypb.RelayService_TunnelClient) {
	defer s.sendWg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-s.sendQ:
			if !ok {
				return
			}
			// Fill sender_id if a caller forgot (defensive)
			if req.GetSenderId() == "" {
				req.SenderId = s.senderID
			}
			// Default topic to current if missing for PUBLISH
			if req.GetType() == relaypb.RelayRequest_PUBLISH && req.GetTopic() == "" {
				req.Topic = s.topic
			}
			if err := stream.Send(req); err != nil {
				// Send failed: let recv loop handle reconnect; drop remaining until reconnect
				return
			}
		}
	}
}

func (s *Session) enqueue(req *relaypb.RelayRequest) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}
	select {
	case s.sendQ <- req:
		return nil
	case <-s.ctx.Done():
		return context.Canceled
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
