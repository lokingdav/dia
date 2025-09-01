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
// Ticket is required only if the topic doesn't exist yet (for topic creation).
// Publishing to existing topics allows empty tickets.
func (s *Server) Publish(ctx context.Context, req *pb.PublishRequest) (*pb.PublishResponse, error) {
	topic := req.GetTopic()
	log.Printf("Received PUBLISH on topic %s", topic)

	response := &pb.PublishResponse{RelayAt: timestamppb.Now()}
	msg := req.GetMessage()

	// Check if topic already exists
	s.mu.RLock()
	_, exists := s.clients[topic]
	s.mu.RUnlock()

	// If topic doesn't exist, ticket is required to create it
	if !exists {
		ticket := req.GetTicket()
		if len(ticket) == 0 {
			return nil, status.Error(codes.PermissionDenied, "Ticket required to create new topic")
		}

		// Verify ticket for topic creation
		ok, err := voprf.VerifyTicket(ticket, s.cfg.AtVerifyKey)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Ticket verification failed: %v", err)
		}
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "Invalid ticket for topic creation")
		}

		// Create the topic since authentication passed
		s.mu.Lock()
		if _, present := s.clients[topic]; !present {
			s.clients[topic] = make(map[*subscriber]struct{})
		}
		s.mu.Unlock()

		log.Printf("Created new topic %s with authenticated ticket", topic)
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

// Subscribe creates/opens the topic, replays history, then joins live delivery.
// No authentication required for subscribers.
func (s *Server) Subscribe(req *pb.SubscribeRequest, stream pb.RelayService_SubscribeServer) error {
	log.Printf("Received SUBSCRIBE on topic %v", req.GetTopic())

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
