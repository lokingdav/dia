// FILE: internal/relay/grpc.go
package relay

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/dense-identity/denseid/api/go/relay/v1"
	dia "github.com/lokingdav/libdia/bindings/go/v2"
	"google.golang.org/grpc/codes"
)

// session holds a client's Tunnel stream and a single-writer queue.
type session struct {
	stream      pb.RelayService_TunnelServer
	ctx         context.Context
	activeTopic string
	sendQ       chan *pb.RelayResponse
	closeOnce   sync.Once
	dropCount   atomic.Uint64
	replaying   atomic.Bool
}

func (s *session) closeQ() { s.closeOnce.Do(func() { close(s.sendQ) }) }

// Server implements RelayServiceServer.
type Server struct {
	pb.UnimplementedRelayServiceServer

	cfg     *Config
	mu      sync.RWMutex
	clients map[string]map[*session]struct{} // topic → set(sessions)
	store   *RedisStore                      // Redis-backed message store
}

func NewServer(cfg *Config) (*Server, error) {
	store, err := NewRedisStore(cfg)
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg:     cfg,
		clients: make(map[string]map[*session]struct{}),
		store:   store,
	}, nil
}

// Close gracefully shuts down the server
func (s *Server) Close() error {
	return s.store.Close()
}

// startWriter launches the sole goroutine allowed to call stream.Send for this session.
func (s *Server) startWriter(sess *session) {
	go func() {
		for m := range sess.sendQ {
			if err := sess.stream.Send(m); err != nil {
				// Stream is broken; detach and stop the writer.
				s.detach(sess)
				return
			}
		}
	}()
}

// detach removes the session from its active topic (if any) and closes its queue.
func (s *Server) detach(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.activeTopic != "" {
		if subs, ok := s.clients[sess.activeTopic]; ok {
			delete(subs, sess)
			if len(subs) == 0 {
				delete(s.clients, sess.activeTopic)
			}
		}
		sess.activeTopic = ""
	}
	sess.closeQ()
}

// ensureTopicBucket creates an empty subscriber set for topic if absent.
func (s *Server) ensureTopicBucket(topic string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.clients[topic]; !ok {
		s.clients[topic] = make(map[*session]struct{})
	}
}

// addToTopic adds sess to topic's live set and sets activeTopic.
func (s *Server) addToTopic(topic string, sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// remove from old topic if different
	if sess.activeTopic != "" && sess.activeTopic != topic {
		if subs, ok := s.clients[sess.activeTopic]; ok {
			delete(subs, sess)
			if len(subs) == 0 {
				delete(s.clients, sess.activeTopic)
			}
		}
	}
	if _, ok := s.clients[topic]; !ok {
		s.clients[topic] = make(map[*session]struct{})
	}
	s.clients[topic][sess] = struct{}{}
	sess.activeTopic = topic
}

// removeFromActive unsubscribes sess from its active topic.
func (s *Server) removeFromActive(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.activeTopic == "" {
		return
	}
	if subs, ok := s.clients[sess.activeTopic]; ok {
		delete(subs, sess)
		if len(subs) == 0 {
			delete(s.clients, sess.activeTopic)
		}
	}
	sess.activeTopic = ""
}

// topicExists checks in-memory or Redis for topic existence.
func (s *Server) topicExists(ctx context.Context, topic string) bool {
	s.mu.RLock()
	_, ok := s.clients[topic]
	s.mu.RUnlock()
	if ok {
		return true
	}
	exists, err := s.store.TopicExists(ctx, topic)
	if err != nil {
		log.Printf("Warning: TopicExists(%s) err: %v", topic, err)
	}
	return exists
}

// Tunnel: the single bidi stream.
func (s *Server) Tunnel(stream pb.RelayService_TunnelServer) error {
	sess := &session{
		stream: stream,
		ctx:    stream.Context(),
		sendQ:  make(chan *pb.RelayResponse, 128),
	}
	s.startWriter(sess)

	for {
		req, err := stream.Recv()
		if err != nil {
			// client closed or stream error
			s.detach(sess)
			return err
		}
		switch req.GetType() {
		case pb.RelayRequest_PUBLISH:
			s.handlePublish(sess, req)
		case pb.RelayRequest_SUBSCRIBE:
			s.handleSubscribe(sess, req)
		default:
			s.sendError(sess, codes.InvalidArgument, "unknown request type", req.GetTopic())
		}
	}
}

func (s *Server) handlePublish(sess *session, req *pb.RelayRequest) {
	topic := req.GetTopic()
	senderId := req.GetSenderId()
	payload := req.GetPayload()

	if topic == "" || len(payload) == 0 {
		log.Printf("PUBLISH %s: missing topic or payload", topic)
		s.sendError(sess, codes.InvalidArgument, "missing topic or payload", topic)
		return
	}

	// PUBLISH never creates a topic. Topics are created via SUBSCRIBE (ticketed).
	if !s.topicExists(sess.ctx, topic) {
		s.sendError(sess, codes.PermissionDenied, "topic does not exist; subscribe first", topic)
		return
	}

	// Snapshot recipients (except the originator) WITHOUT holding the lock during sends.
	var recipients []*session
	s.mu.RLock()
	if subs, ok := s.clients[topic]; ok {
		for sb := range subs {
			// Self-echo suppression: compare per-frame sender_id.
			if req.GetSenderId() == "" || req.GetSenderId() != "" {
				// We don't track per-session sender_id; suppress by stream identity not available.
				// Instead, rely on senderId comparison at delivery time: pass senderId through and check in send loop.
				// Easiest here: do not filter; we'll filter by senderId when storing and when replaying.
			}
			recipients = append(recipients, sb)
		}
	}
	s.mu.RUnlock()

	msg, parseErr := dia.ParseMessage(payload)
	isTerminal := parseErr == nil && msg != nil && (msg.Type() == dia.MsgAKEComplete || msg.Type() == dia.MsgBye)

	// Persist history for non-terminal frames only.
	if !isTerminal {
		if err := s.store.StoreMessage(sess.ctx, topic, payload, senderId); err != nil {
			log.Printf("Warning: StoreMessage(%s) failed: %v", topic, err)
		}
	}

	// Fan-out EVENT to subscribers, suppressing self by comparing stream identity:
	for _, sb := range recipients {
		// We can't know the subscribing client's sender_id here; we suppress self by comparing stream pointer.
		if sb == sess {
			continue
		}
		// If a subscriber is currently replaying history for this topic, skip live delivery.
		// It will catch up from Redis after replay completes.
		if sb.replaying.Load() {
			continue
		}
		resp := &pb.RelayResponse{
			Type:    pb.RelayResponse_EVENT,
			Topic:   topic,
			Payload: payload,
		}
		s.tryEnqueue(sb, resp)
	}

	// Clear stored history after delivering terminal frames to live subscribers.
	if isTerminal {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.store.DeleteTopic(ctx, topic); err != nil {
			log.Printf("Warning: DeleteTopic(%s) failed: %v", topic, err)
		} else {
			log.Printf("DIA term %s type=%d: cleared topic history", topic, msg.Type())
		}
	}
	log.Printf("PUBLISH %s: ok (sender=%q sess=%p fanned to %d)", topic, senderId, sess, len(recipients))
}

func (s *Server) handleSubscribe(sess *session, req *pb.RelayRequest) {
	target := req.GetTopic()
	if target == "" {
		log.Printf("SUBSCRIBE: missing topic")
		s.sendError(sess, codes.InvalidArgument, "missing topic", "")
		return
	}

	// Every SUBSCRIBE consumes a ticket/token.
	ticket := req.GetTicket()
	if len(ticket) == 0 {
		s.sendError(sess, codes.PermissionDenied, "ticket required to subscribe", target)
		return
	}
	ok, err := dia.VerifyTicket(ticket, s.cfg.AtVerifyKey)
	if err != nil {
		s.sendError(sess, codes.Internal, "ticket verification failed", target)
		return
	}
	if !ok {
		s.sendError(sess, codes.Unauthenticated, "invalid ticket for subscribe", target)
		return
	}

	hasPayload := len(req.GetPayload()) > 0

	// If this is the first time the topic is seen, create it (ticket already verified).
	if !s.topicExists(sess.ctx, target) {
		s.ensureTopicBucket(target)
		if err := s.store.CreateTopic(sess.ctx, target); err != nil {
			log.Printf("Warning: CreateTopic(%s): %v", target, err)
		}
	} else {
		// Ensure in-memory bucket exists for live fan-out.
		s.ensureTopicBucket(target)
	}

	// Implicitly detach from previous active topic (local to this session)
	if sess.activeTopic != "" && sess.activeTopic != target {
		s.removeFromActive(sess)
	}

	// REPLAY history. To avoid missing messages published concurrently with replay,
	// we temporarily suppress live delivery to this session and do a catch-up
	// fetch after replay completes.
	sess.replaying.Store(true)

	// Snapshot history before joining live delivery.
	hist, err := s.store.GetMessageHistory(sess.ctx, target)
	if err != nil {
		log.Printf("Warning: GetMessageHistory(%s): %v", target, err)
		hist = nil
	}

	// Join live delivery
	s.addToTopic(target, sess)

	for _, m := range hist {
		if req.GetSenderId() != "" && m.SenderID == req.GetSenderId() {
			continue
		}
		s.tryEnqueue(sess, &pb.RelayResponse{
			Type:    pb.RelayResponse_EVENT,
			Topic:   target,
			Payload: m.Payload,
		})
	}

	// Catch up any messages that were stored while we were replaying and before
	// we re-enabled live delivery.
	hist2, err2 := s.store.GetMessageHistory(sess.ctx, target)
	if err2 != nil {
		log.Printf("Warning: GetMessageHistory(catchup %s): %v", target, err2)
		hist2 = nil
	}
	if len(hist2) > len(hist) {
		for _, m := range hist2[len(hist):] {
			if req.GetSenderId() != "" && m.SenderID == req.GetSenderId() {
				continue
			}
			s.tryEnqueue(sess, &pb.RelayResponse{
				Type:    pb.RelayResponse_EVENT,
				Topic:   target,
				Payload: m.Payload,
			})
		}
	}

	sess.replaying.Store(false)

	log.Printf("SUBSCRIBE %s: ok (sender=%q sess=%p)", target, req.GetSenderId(), sess)

	// Piggy-back publish if payload present
	if hasPayload {
		s.handlePublish(sess, req)
	}
}

// tryEnqueue attempts a non-blocking send; drops on backpressure.
func (s *Server) tryEnqueue(sess *session, resp *pb.RelayResponse) {
	defer func() { _ = recover() }() // in case sendQ is closed
	select {
	case sess.sendQ <- resp:
	default:
		// POC backpressure: drop instead of blocking
		cnt := sess.dropCount.Add(1)
		// Log early drops to diagnose protocol-critical message loss without spamming.
		if cnt <= 10 || cnt%1000 == 0 {
			log.Printf("Warning: drop resp type=%v topic=%s sess=%p (dropCount=%d)", resp.GetType(), resp.GetTopic(), sess, cnt)
		}
	}
}

// sendError emits a one-off ERROR response to this session.
func (s *Server) sendError(sess *session, code codes.Code, msg string, topic string) {
	resp := &pb.RelayResponse{
		Type:    pb.RelayResponse_ERROR,
		Topic:   topic,
		Code:    int32(code),
		Message: msg,
	}
	s.tryEnqueue(sess, resp)
}
