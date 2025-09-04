// FILE: internal/relay/grpc.go
package relay

import (
	"context"
	"log"
	"sync"

	pb "github.com/dense-identity/denseid/api/go/relay/v1"
	"github.com/dense-identity/denseid/internal/voprf"
	"google.golang.org/grpc/codes"
)

// session holds a client's Tunnel stream and a single-writer queue.
type session struct {
	stream      pb.RelayService_TunnelServer
	ctx         context.Context
	activeTopic string
	sendQ       chan *pb.RelayResponse
	closeOnce   sync.Once
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
		case pb.RelayRequest_UNSUBSCRIBE:
			s.handleUnsubscribe(sess, req)
		case pb.RelayRequest_SWAP:
			s.handleSwap(sess, req)
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
		s.sendError(sess, codes.InvalidArgument, "missing topic or payload", topic)
		return
	}

	// Check existence; require ticket only if topic doesn't exist.
	if !s.topicExists(sess.ctx, topic) {
		ticket := req.GetTicket()
		if len(ticket) == 0 {
			s.sendError(sess, codes.PermissionDenied, "ticket required to create new topic", topic)
			return
		}
		ok, err := voprf.VerifyTicket(ticket, s.cfg.AtVerifyKey)
		if err != nil {
			s.sendError(sess, codes.Internal, "ticket verification failed", topic)
			return
		}
		if !ok {
			s.sendError(sess, codes.Unauthenticated, "invalid ticket for topic creation", topic)
			return
		}
		// Mark existence: create in-memory bucket and Redis tracking.
		s.ensureTopicBucket(topic)
		if err := s.store.CreateTopic(sess.ctx, topic); err != nil {
			log.Printf("Warning: CreateTopic(%s): %v", topic, err)
		}
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

	// Store message in Redis with TTL
	if err := s.store.StoreMessage(sess.ctx, topic, payload, senderId); err != nil {
		log.Printf("Warning: StoreMessage(%s) failed: %v", topic, err)
	}

	// Fan-out EVENT to subscribers, suppressing self by comparing stream identity:
	for _, sb := range recipients {
		// We can't know the subscribing client's sender_id here; we suppress self by comparing stream pointer.
		if sb == sess {
			continue
		}
		resp := &pb.RelayResponse{
			Type:    pb.RelayResponse_EVENT,
			Topic:   topic,
			Payload: payload,
		}
		s.tryEnqueue(sb, resp)
	}
}

func (s *Server) handleSubscribe(sess *session, req *pb.RelayRequest) {
	target := req.GetTopic()
	if target == "" {
		s.sendError(sess, codes.InvalidArgument, "missing topic", "")
		return
	}

	// If payload is present AND topic doesn't exist, require valid ticket; on failure, do nothing else.
	hasPayload := len(req.GetPayload()) > 0
	if hasPayload && !s.topicExists(sess.ctx, target) {
		ticket := req.GetTicket()
		if len(ticket) == 0 {
			s.sendError(sess, codes.PermissionDenied, "ticket required for publish on new topic", target)
			return
		}
		ok, err := voprf.VerifyTicket(ticket, s.cfg.AtVerifyKey)
		if err != nil {
			s.sendError(sess, codes.Internal, "ticket verification failed", target)
			return
		}
		if !ok {
			s.sendError(sess, codes.Unauthenticated, "invalid ticket for topic creation", target)
			return
		}
		// Mark existence ahead of replay and piggy-back publish
		s.ensureTopicBucket(target)
		if err := s.store.CreateTopic(sess.ctx, target); err != nil {
			log.Printf("Warning: CreateTopic(%s): %v", target, err)
		}
	} else {
		// Ensure in-memory bucket exists (subscribe without payload can create topic without ticket)
		s.ensureTopicBucket(target)
	}

	// Implicitly unsubscribe from previous active topic (local to this session)
	if sess.activeTopic != "" && sess.activeTopic != target {
		s.removeFromActive(sess)
	}

	// REPLAY history (skip sender's own messages)
	hist, err := s.store.GetMessageHistory(sess.ctx, target)
	if err != nil {
		log.Printf("Warning: GetMessageHistory(%s): %v", target, err)
		hist = nil
	}
	for _, m := range hist {
		if m.SenderID == req.GetSenderId() {
			continue
		}
		s.tryEnqueue(sess, &pb.RelayResponse{
			Type:    pb.RelayResponse_EVENT,
			Topic:   target,
			Payload: m.Payload,
		})
	}

	// Join live delivery
	s.addToTopic(target, sess)

	// Piggy-back publish if payload present
	if hasPayload {
		s.handlePublish(sess, req)
	}
}

func (s *Server) handleUnsubscribe(sess *session, req *pb.RelayRequest) {
	// Only affects this session
	if sess.activeTopic == "" {
		return
	}
	if req.GetTopic() != "" && req.GetTopic() != sess.activeTopic {
		// If a topic is provided and doesn't match current, treat as invalid.
		s.sendError(sess, codes.InvalidArgument, "not subscribed to the specified topic", req.GetTopic())
		return
	}
	s.removeFromActive(sess)
}

func (s *Server) handleSwap(sess *session, req *pb.RelayRequest) {
	from := req.GetTopic()
	to := req.GetToTopic()
	if from == "" || to == "" {
		s.sendError(sess, codes.InvalidArgument, "missing from/to topic", "")
		return
	}
	// Require from == current active topic
	if sess.activeTopic == "" || sess.activeTopic != from {
		s.sendError(sess, codes.InvalidArgument, "swap from-topic does not match current subscription", from)
		return
	}

	// Ticket only if payload present AND target doesn't exist
	hasPayload := len(req.GetPayload()) > 0
	if hasPayload && !s.topicExists(sess.ctx, to) {
		ticket := req.GetTicket()
		if len(ticket) == 0 {
			s.sendError(sess, codes.PermissionDenied, "ticket required for publish on new topic", to)
			return
		}
		ok, err := voprf.VerifyTicket(ticket, s.cfg.AtVerifyKey)
		if err != nil {
			s.sendError(sess, codes.Internal, "ticket verification failed", to)
			return
		}
		if !ok {
			s.sendError(sess, codes.Unauthenticated, "invalid ticket for topic creation", to)
			return
		}
		// Mark existence
		s.ensureTopicBucket(to)
		if err := s.store.CreateTopic(sess.ctx, to); err != nil {
			log.Printf("Warning: CreateTopic(%s): %v", to, err)
		}
	} else {
		// Ensure in-memory bucket exists so the topic now "exists"
		s.ensureTopicBucket(to)
	}

	// REPLAY new topic
	hist, err := s.store.GetMessageHistory(sess.ctx, to)
	if err != nil {
		log.Printf("Warning: GetMessageHistory(%s): %v", to, err)
		hist = nil
	}
	for _, m := range hist {
		if m.SenderID == req.GetSenderId() {
			continue
		}
		s.tryEnqueue(sess, &pb.RelayResponse{
			Type:    pb.RelayResponse_EVENT,
			Topic:   to,
			Payload: m.Payload,
		})
	}

	// Atomically move: remove from old, add to new
	s.addToTopic(to, sess)

	// Piggy-back publish (to the new topic) if provided
	if hasPayload {
		// reuse handlePublish with topic=to
		pub := &pb.RelayRequest{
			SenderId: req.GetSenderId(),
			Type:     pb.RelayRequest_PUBLISH,
			Topic:    to,
			Payload:  req.GetPayload(),
			Ticket:   req.GetTicket(),
		}
		s.handlePublish(sess, pub)
	}
}

// tryEnqueue attempts a non-blocking send; drops on backpressure.
func (s *Server) tryEnqueue(sess *session, resp *pb.RelayResponse) {
	defer func() { _ = recover() }() // in case sendQ is closed
	select {
	case sess.sendQ <- resp:
	default:
		// POC backpressure: drop instead of blocking
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
