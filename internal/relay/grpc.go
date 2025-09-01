// FILE: relay/server.go
package relay

import (
	"context"
	"log"
	"sync"

	pb "github.com/dense-identity/denseid/api/go/relay/v1"
	"github.com/dense-identity/denseid/internal/voprf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// subscriber holds each client’s stream and its sender ID, plus a single-writer queue.
type subscriber struct {
	stream    pb.RelayService_SubscribeServer
	ctx       context.Context
	senderId  string
	sendQ     chan *pb.RelayMessage
	closeOnce sync.Once
}

func (s *subscriber) closeQ() { s.closeOnce.Do(func() { close(s.sendQ) }) }

// Server implements RelayServiceServer.
type Server struct {
	pb.UnimplementedRelayServiceServer

	cfg        *Config
	mu         sync.RWMutex
	clients    map[string]map[*subscriber]struct{} // topic → set(subscribers)
	history    map[string][]*pb.RelayMessage       // topic → last N msgs
	maxHistory int
}

func NewServer(cfg *Config) *Server {
	return &Server{
		cfg:        cfg,
		clients:    make(map[string]map[*subscriber]struct{}),
		history:    make(map[string][]*pb.RelayMessage),
		maxHistory: 100,
	}
}

// removeSubscriber is idempotent: detaches sub from topic set and closes its queue.
func (s *Server) removeSubscriber(topic string, sub *subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if subs, ok := s.clients[topic]; ok {
		if _, ok := subs[sub]; ok {
			delete(subs, sub)
			if len(subs) == 0 {
				// No more subscribers → topic ceases to "exist" for unauthenticated publishers.
				delete(s.clients, topic)
			}
		}
	}
	sub.closeQ()
}

// startWriter launches the sole goroutine allowed to call stream.Send for this subscriber.
func (s *Server) startWriter(topic string, sub *subscriber) {
	go func() {
		for m := range sub.sendQ {
			if err := sub.stream.Send(m); err != nil {
				// Stream is broken; remove subscriber and stop the writer.
				s.removeSubscriber(topic, sub)
				return
			}
		}
	}()
}

// Publish handles a message and broadcasts it to existing topic subscribers (except originator).
// Anyone may publish, BUT ONLY if the topic already exists (created by a prior Subscribe).
func (s *Server) Publish(ctx context.Context, msg *pb.RelayMessage) (*pb.PublishResponse, error) {
	topic := msg.GetTopic()
	log.Printf("Received PUBLISH on topic %s", topic)
	response := &pb.PublishResponse{RelayAt: timestamppb.Now()}

	// Topic must already exist (Subscribe creates it). Otherwise return silently.
	s.mu.RLock()
	_, exists := s.clients[topic]
	s.mu.RUnlock()
	if !exists {
		return response, nil
	}

	// Snapshot recipients (except the originator) WITHOUT holding the lock during sends.
	var recipients []*subscriber
	s.mu.RLock()
	if subs, ok := s.clients[topic]; ok {
		for sub := range subs {
			if sub.senderId == msg.GetSenderId() {
				continue
			}
			recipients = append(recipients, sub)
		}
	}
	s.mu.RUnlock()

	// Append to history (ring-buffer style).
	s.mu.Lock()
	h := append(s.history[topic], msg)
	if len(h) > s.maxHistory {
		h = h[len(h)-s.maxHistory:]
	}
	s.history[topic] = h
	s.mu.Unlock()

	// Enqueue to each subscriber's writer queue.
	// Drop if their queue is full or already closed (POC-friendly semantics).
	for _, sub := range recipients {
		func(sb *subscriber) {
			defer func() { _ = recover() }() // handle send on closed chan (race-safe)
			select {
			case sb.sendQ <- msg:
			default:
				// Backpressure policy for POC: drop instead of blocking publisher.
			}
		}(sub)
	}

	return response, nil
}

// Subscribe authenticates, creates/opens the topic, replays history, then joins live delivery.
func (s *Server) Subscribe(req *pb.SubscribeRequest, stream pb.RelayService_SubscribeServer) error {
	log.Printf("Received SUBSCRIBE on topic %v", req.GetTopic())

	// 1) Verify ticket (binds authorization to open the channel).
	ok, err := voprf.VerifyTicket(req.GetTicket(), s.cfg.AtVerifyKey)
	if err != nil {
		return status.Errorf(codes.Internal, "Unauthorized: %v", err)
	}
	if !ok {
		return status.Error(codes.InvalidArgument, "Invalid ticket")
	}

	topic := req.GetTopic()
	sub := &subscriber{
		stream:   stream,
		ctx:      stream.Context(),
		senderId: req.GetSenderId(),
		sendQ:    make(chan *pb.RelayMessage, 128),
	}

	// 2) Ensure the topic "exists" immediately (so unauthenticated Publish sees it),
	//    but DO NOT join live delivery yet (avoid interleaving during replay).
	s.mu.Lock()
	if _, present := s.clients[topic]; !present {
		s.clients[topic] = make(map[*subscriber]struct{})
	}
	// Snapshot history now; we'll replay it before joining live.
	histCopy := append([]*pb.RelayMessage(nil), s.history[topic]...)
	s.mu.Unlock()

	// 3) Replay history to this subscriber (skip their own past messages if desired).
	for _, m := range histCopy {
		if m.GetSenderId() == sub.senderId {
			continue
		}
		if err := stream.Send(m); err != nil {
			// Stream failed during replay; nothing to clean (not added to clients yet).
			return err
		}
	}

	// 4) Join live delivery and start the dedicated writer.
	s.mu.Lock()
	s.clients[topic][sub] = struct{}{}
	s.mu.Unlock()
	s.startWriter(topic, sub)

	// 5) Block until the client disconnects; then cleanup.
	<-sub.ctx.Done()
	s.removeSubscriber(topic, sub)
	return nil
}
