package relay

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"

	pb "github.com/dense-identity/denseid/api/go/relay/v1"
	"github.com/dense-identity/denseid/internal/voprf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// streamClient represents a single bidirectional stream connection
type streamClient struct {
	stream    pb.RelayService_RelayServer
	ctx       context.Context
	senderID  string
	sendQ     chan *pb.RelayResponse
	closeOnce sync.Once

	// Topic subscriptions for this client
	mu            sync.RWMutex
	subscriptions map[string]bool // topic -> subscribed
}

func newStreamClient(stream pb.RelayService_RelayServer, senderID string) *streamClient {
	return &streamClient{
		stream:        stream,
		ctx:           stream.Context(),
		senderID:      senderID,
		sendQ:         make(chan *pb.RelayResponse, 256), // Larger buffer for bidirectional
		subscriptions: make(map[string]bool),
	}
}

func (sc *streamClient) closeQ() {
	sc.closeOnce.Do(func() { close(sc.sendQ) })
}

func (sc *streamClient) isSubscribedTo(topic string) bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.subscriptions[topic]
}

func (sc *streamClient) addSubscription(topic string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.subscriptions[topic] = true
}

func (sc *streamClient) removeSubscription(topic string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.subscriptions, topic)
}

func (sc *streamClient) getSubscriptions() []string {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	topics := make([]string, 0, len(sc.subscriptions))
	for topic := range sc.subscriptions {
		topics = append(topics, topic)
	}
	return topics
}

// StreamServer implements the new bidirectional streaming relay service
type StreamServer struct {
	pb.UnimplementedRelayServiceServer

	cfg   *Config
	mu    sync.RWMutex
	store *RedisStore

	// Track clients and their topic subscriptions
	clients    map[*streamClient]struct{}            // All connected clients
	topicSubs  map[string]map[*streamClient]struct{} // topic -> set(clients)
	clientByID map[string]*streamClient              // senderID -> client (for direct messaging if needed)
}

func NewStreamServer(cfg *Config) (*StreamServer, error) {
	store, err := NewRedisStore(cfg)
	if err != nil {
		return nil, err
	}

	return &StreamServer{
		cfg:        cfg,
		store:      store,
		clients:    make(map[*streamClient]struct{}),
		topicSubs:  make(map[string]map[*streamClient]struct{}),
		clientByID: make(map[string]*streamClient),
	}, nil
}

func (s *StreamServer) Close() error {
	return s.store.Close()
}

// Relay handles the bidirectional streaming RPC
func (s *StreamServer) Relay(stream pb.RelayService_RelayServer) error {
	log.Printf("New relay stream connection established")

	var client *streamClient
	clientReady := make(chan struct{})

	// Start goroutines for handling the stream
	ctx := stream.Context()
	errCh := make(chan error, 2)

	// Receive loop - handles incoming requests from client
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in receive loop: %v", r)
				errCh <- fmt.Errorf("receive loop panic: %v", r)
			}
		}()

		for {
			req, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					log.Printf("Client closed the stream")
					errCh <- nil
					return
				}
				log.Printf("Error receiving from stream: %v", err)
				errCh <- err
				return
			}

			// Initialize client on first message
			if client == nil {
				client = newStreamClient(stream, req.GetSenderId())
				s.registerClient(client)
				log.Printf("Registered client %s", req.GetSenderId())
				close(clientReady) // Signal that client is ready
			}

			// Handle the request
			if err := s.handleRequest(ctx, client, req); err != nil {
				log.Printf("Error handling request: %v", err)
				// Send error response but continue
				s.sendErrorResponse(client, req.GetType(), "", err)
			}
		}
	}()

	// Send loop - handles outgoing responses to client
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in send loop: %v", r)
				errCh <- fmt.Errorf("send loop panic: %v", r)
			}
		}()

		// Wait for client to be initialized
		select {
		case <-clientReady:
			// Client is now ready
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		}

		// Now that client is initialized, handle outgoing messages
		for resp := range client.sendQ {
			if err := stream.Send(resp); err != nil {
				log.Printf("Error sending to stream: %v", err)
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()

	// Wait for either goroutine to finish
	err := <-errCh

	// Cleanup
	if client != nil {
		s.unregisterClient(client)
		log.Printf("Unregistered client %s", client.senderID)
	}

	return err
}

func (s *StreamServer) registerClient(client *streamClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clients[client] = struct{}{}
	if client.senderID != "" {
		s.clientByID[client.senderID] = client
	}
}

func (s *StreamServer) unregisterClient(client *streamClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove from all topic subscriptions
	for topic := range client.subscriptions {
		if subs, ok := s.topicSubs[topic]; ok {
			delete(subs, client)
			if len(subs) == 0 {
				delete(s.topicSubs, topic)
			}
		}
	}

	// Remove from client tracking
	delete(s.clients, client)
	if client.senderID != "" {
		delete(s.clientByID, client.senderID)
	}

	client.closeQ()
}

func (s *StreamServer) handleRequest(ctx context.Context, client *streamClient, req *pb.RelayRequest) error {
	switch req.GetType() {
	case pb.RelayMessageType_RELAY_MESSAGE_TYPE_HELLO:
		return s.handleHello(ctx, client, req.GetHello())

	case pb.RelayMessageType_RELAY_MESSAGE_TYPE_PUBLISH:
		return s.handlePublish(ctx, client, req.GetPublish())

	case pb.RelayMessageType_RELAY_MESSAGE_TYPE_SUBSCRIBE:
		return s.handleSubscribe(ctx, client, req.GetSubscribe())

	case pb.RelayMessageType_RELAY_MESSAGE_TYPE_UNSUBSCRIBE:
		return s.handleUnsubscribe(ctx, client, req.GetUnsubscribe())

	case pb.RelayMessageType_RELAY_MESSAGE_TYPE_REPLACE:
		return s.handleReplace(ctx, client, req.GetReplace())

	case pb.RelayMessageType_RELAY_MESSAGE_TYPE_BYE:
		return s.handleBye(ctx, client, req.GetBye())

	default:
		return status.Errorf(codes.InvalidArgument, "unknown message type: %v", req.GetType())
	}
}

func (s *StreamServer) handleHello(ctx context.Context, client *streamClient, op *pb.HelloOperation) error {
	if op == nil {
		return status.Error(codes.InvalidArgument, "hello operation is nil")
	}

	// Hello message is just for initial client registration
	// The actual registration happens when the client is created in the receive loop
	// So we just send back an acknowledgment
	ackResp := &pb.RelayResponse{
		Type: pb.RelayMessageType_RELAY_MESSAGE_TYPE_HELLO,
		Response: &pb.RelayResponse_Ack{
			Ack: &pb.OperationAck{
				OperationType: pb.RelayMessageType_RELAY_MESSAGE_TYPE_HELLO,
				Success:       true,
				Message:       "Client registered successfully",
			},
		},
	}

	select {
	case client.sendQ <- ackResp:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *StreamServer) handlePublish(ctx context.Context, client *streamClient, op *pb.PublishOperation) error {
	if op == nil {
		return status.Error(codes.InvalidArgument, "publish operation is nil")
	}

	topic := op.GetTopic()
	log.Printf("Client %s publishing to topic %s", client.senderID, topic)

	// Check if topic exists
	s.mu.RLock()
	_, exists := s.topicSubs[topic]
	s.mu.RUnlock()

	if !exists {
		// Check Redis for topic existence
		redisExists, err := s.store.TopicExists(ctx, topic)
		if err != nil {
			log.Printf("Warning: failed to check topic in Redis: %v", err)
		}
		exists = redisExists
	}

	// If topic doesn't exist, ticket is required
	if !exists {
		ticket := op.GetTicket()
		if len(ticket) == 0 {
			return status.Error(codes.PermissionDenied, "Ticket required to create new topic")
		}

		// Verify ticket
		ok, err := voprf.VerifyTicket(ticket, s.cfg.AtVerifyKey)
		if err != nil {
			return status.Errorf(codes.Internal, "Ticket verification failed: %v", err)
		}
		if !ok {
			return status.Error(codes.Unauthenticated, "Invalid ticket for topic creation")
		}

		// Create topic
		s.mu.Lock()
		if _, present := s.topicSubs[topic]; !present {
			s.topicSubs[topic] = make(map[*streamClient]struct{})
		}
		s.mu.Unlock()

		if err := s.store.CreateTopic(ctx, topic); err != nil {
			log.Printf("Warning: failed to create topic in Redis: %v", err)
		}

		log.Printf("Created new topic %s with authenticated ticket", topic)
	}

	// Create message
	message := &pb.MessageDelivery{
		Topic:     topic,
		Payload:   op.GetPayload(),
		SenderId:  client.senderID,
		Timestamp: timestamppb.Now(),
		MessageId: op.GetMessageId(),
	}

	// Store in Redis
	if err := s.store.StoreMessage(ctx, topic, message); err != nil {
		log.Printf("Warning: failed to store message in Redis: %v", err)
	}

	// Get subscribers (excluding sender)
	var recipients []*streamClient
	s.mu.RLock()
	if subs, ok := s.topicSubs[topic]; ok {
		for sub := range subs {
			if sub.senderID != client.senderID && sub.isSubscribedTo(topic) {
				recipients = append(recipients, sub)
			}
		}
	}
	s.mu.RUnlock()

	// Deliver to subscribers
	response := &pb.RelayResponse{
		Type: pb.RelayMessageType_RELAY_MESSAGE_TYPE_PUBLISH,
		Response: &pb.RelayResponse_Message{
			Message: message,
		},
	}

	for _, recipient := range recipients {
		select {
		case recipient.sendQ <- response:
		default:
			// Drop message if queue is full (backpressure policy)
			log.Printf("Dropping message to client %s due to full queue", recipient.senderID)
		}
	}

	// Send ack to publisher
	ack := &pb.RelayResponse{
		Type: pb.RelayMessageType_RELAY_MESSAGE_TYPE_PUBLISH,
		Response: &pb.RelayResponse_Ack{
			Ack: &pb.OperationAck{
				OperationType: pb.RelayMessageType_RELAY_MESSAGE_TYPE_PUBLISH,
				Topic:         topic,
				Success:       true,
				Message:       "Message published successfully",
				Timestamp:     timestamppb.Now(),
			},
		},
	}

	select {
	case client.sendQ <- ack:
	default:
		// Client queue full - this is unusual for acks
		log.Printf("Warning: Could not send publish ack to client %s", client.senderID)
	}

	return nil
}

func (s *StreamServer) handleSubscribe(ctx context.Context, client *streamClient, op *pb.SubscribeOperation) error {
	if op == nil {
		return status.Error(codes.InvalidArgument, "subscribe operation is nil")
	}

	topic := op.GetTopic()
	log.Printf("Client %s subscribing to topic %s", client.senderID, topic)

	// Add to topic subscriptions
	s.mu.Lock()
	if _, present := s.topicSubs[topic]; !present {
		s.topicSubs[topic] = make(map[*streamClient]struct{})
	}
	s.topicSubs[topic][client] = struct{}{}
	s.mu.Unlock()

	// Track subscription in client
	client.addSubscription(topic)

	// Replay history if requested
	if op.GetReplayHistory() {
		history, err := s.store.GetMessageHistory(ctx, topic)
		if err != nil {
			log.Printf("Warning: failed to get message history for topic %s: %v", topic, err)
		} else {
			for _, msg := range history {
				if msg.GetSenderId() == client.senderID {
					continue // Skip own messages
				}

				response := &pb.RelayResponse{
					Type: pb.RelayMessageType_RELAY_MESSAGE_TYPE_SUBSCRIBE,
					Response: &pb.RelayResponse_Message{
						Message: msg, // msg is already a *pb.MessageDelivery
					},
				}

				select {
				case client.sendQ <- response:
				default:
					log.Printf("Warning: Could not replay message to client %s", client.senderID)
				}
			}
		}
	}

	// Send subscription ack
	ack := &pb.RelayResponse{
		Type: pb.RelayMessageType_RELAY_MESSAGE_TYPE_SUBSCRIBE,
		Response: &pb.RelayResponse_Ack{
			Ack: &pb.OperationAck{
				OperationType: pb.RelayMessageType_RELAY_MESSAGE_TYPE_SUBSCRIBE,
				Topic:         topic,
				Success:       true,
				Message:       "Subscribed successfully",
				Timestamp:     timestamppb.Now(),
			},
		},
	}

	select {
	case client.sendQ <- ack:
	default:
		log.Printf("Warning: Could not send subscribe ack to client %s", client.senderID)
	}

	return nil
}

func (s *StreamServer) handleUnsubscribe(ctx context.Context, client *streamClient, op *pb.UnsubscribeOperation) error {
	if op == nil {
		return status.Error(codes.InvalidArgument, "unsubscribe operation is nil")
	}

	topic := op.GetTopic()
	log.Printf("Client %s unsubscribing from topic %s", client.senderID, topic)

	// Remove from topic subscriptions
	s.mu.Lock()
	if subs, ok := s.topicSubs[topic]; ok {
		delete(subs, client)
		if len(subs) == 0 {
			delete(s.topicSubs, topic)
		}
	}
	s.mu.Unlock()

	// Remove from client's subscription tracking
	client.removeSubscription(topic)

	// Send unsubscribe ack
	ack := &pb.RelayResponse{
		Type: pb.RelayMessageType_RELAY_MESSAGE_TYPE_UNSUBSCRIBE,
		Response: &pb.RelayResponse_Ack{
			Ack: &pb.OperationAck{
				OperationType: pb.RelayMessageType_RELAY_MESSAGE_TYPE_UNSUBSCRIBE,
				Topic:         topic,
				Success:       true,
				Message:       "Unsubscribed successfully",
				Timestamp:     timestamppb.Now(),
			},
		},
	}

	select {
	case client.sendQ <- ack:
	default:
		log.Printf("Warning: Could not send unsubscribe ack to client %s", client.senderID)
	}

	return nil
}

func (s *StreamServer) handleReplace(ctx context.Context, client *streamClient, op *pb.ReplaceOperation) error {
	if op == nil {
		return status.Error(codes.InvalidArgument, "replace operation is nil")
	}

	oldTopic := op.GetOldTopic()
	newTopic := op.GetNewTopic()
	log.Printf("Client %s replacing topic %s with %s", client.senderID, oldTopic, newTopic)

	// Unsubscribe from old topic
	if oldTopic != "" {
		unsubOp := &pb.UnsubscribeOperation{Topic: oldTopic}
		if err := s.handleUnsubscribe(ctx, client, unsubOp); err != nil {
			return fmt.Errorf("failed to unsubscribe from old topic: %w", err)
		}
	}

	// Subscribe to new topic
	subOp := &pb.SubscribeOperation{
		Topic:         newTopic,
		ReplayHistory: op.GetReplayHistory(),
	}

	// For new topic creation, we need to handle tickets
	if op.GetTicket() != nil {
		// Check if new topic exists
		s.mu.RLock()
		_, exists := s.topicSubs[newTopic]
		s.mu.RUnlock()

		if !exists {
			redisExists, err := s.store.TopicExists(ctx, newTopic)
			if err != nil {
				log.Printf("Warning: failed to check topic in Redis: %v", err)
			}
			exists = redisExists
		}

		if !exists {
			// Verify ticket for new topic creation
			ok, err := voprf.VerifyTicket(op.GetTicket(), s.cfg.AtVerifyKey)
			if err != nil {
				return status.Errorf(codes.Internal, "Ticket verification failed: %v", err)
			}
			if !ok {
				return status.Error(codes.Unauthenticated, "Invalid ticket for topic creation")
			}

			// Create the new topic
			s.mu.Lock()
			if _, present := s.topicSubs[newTopic]; !present {
				s.topicSubs[newTopic] = make(map[*streamClient]struct{})
			}
			s.mu.Unlock()

			if err := s.store.CreateTopic(ctx, newTopic); err != nil {
				log.Printf("Warning: failed to create topic in Redis: %v", err)
			}

			log.Printf("Created new topic %s during replace operation", newTopic)
		}
	}

	if err := s.handleSubscribe(ctx, client, subOp); err != nil {
		return fmt.Errorf("failed to subscribe to new topic: %w", err)
	}

	// Send replace ack
	ack := &pb.RelayResponse{
		Type: pb.RelayMessageType_RELAY_MESSAGE_TYPE_REPLACE,
		Response: &pb.RelayResponse_Ack{
			Ack: &pb.OperationAck{
				OperationType: pb.RelayMessageType_RELAY_MESSAGE_TYPE_REPLACE,
				Topic:         newTopic,
				Success:       true,
				Message:       fmt.Sprintf("Replaced %s with %s successfully", oldTopic, newTopic),
				Timestamp:     timestamppb.Now(),
			},
		},
	}

	select {
	case client.sendQ <- ack:
	default:
		log.Printf("Warning: Could not send replace ack to client %s", client.senderID)
	}

	return nil
}

func (s *StreamServer) handleBye(ctx context.Context, client *streamClient, op *pb.ByeOperation) error {
	reason := "Client requested disconnection"
	if op != nil && op.GetReason() != "" {
		reason = op.GetReason()
	}

	log.Printf("Client %s disconnecting: %s", client.senderID, reason)

	// Send bye ack
	ack := &pb.RelayResponse{
		Type: pb.RelayMessageType_RELAY_MESSAGE_TYPE_BYE,
		Response: &pb.RelayResponse_Ack{
			Ack: &pb.OperationAck{
				OperationType: pb.RelayMessageType_RELAY_MESSAGE_TYPE_BYE,
				Success:       true,
				Message:       "Goodbye",
				Timestamp:     timestamppb.Now(),
			},
		},
	}

	select {
	case client.sendQ <- ack:
	default:
		// Client may have already disconnected
	}

	// Close the send queue to trigger disconnection
	client.closeQ()

	return io.EOF // Signal end of stream
}

func (s *StreamServer) sendErrorResponse(client *streamClient, opType pb.RelayMessageType, topic string, err error) {
	errorResp := &pb.RelayResponse{
		Type: opType,
		Response: &pb.RelayResponse_Error{
			Error: &pb.ErrorResponse{
				OperationType: opType,
				Topic:         topic,
				ErrorCode:     status.Code(err).String(),
				ErrorMessage:  err.Error(),
				Timestamp:     timestamppb.Now(),
			},
		},
	}

	select {
	case client.sendQ <- errorResp:
	default:
		log.Printf("Warning: Could not send error response to client %s", client.senderID)
	}
}
