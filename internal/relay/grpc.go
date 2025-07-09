package relay

import (
    "context"
    "sync"

    pb "github.com/dense-identity/denseid/api/go/relay/v1"
    "github.com/dense-identity/denseid/internal/signing"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/timestamppb"
)

// subscriber holds each client’s stream and its sender ID
type subscriber struct {
    stream   pb.RelayService_SubscribeServer
    ctx      context.Context
    senderID string
}

// Server implements RelayServiceServer
type Server struct {
    pb.UnimplementedRelayServiceServer

    cfg        *Config
    mu         sync.RWMutex
    clients    map[string]map[*subscriber]struct{}   // channel → set of subs
    history    map[string][]*pb.RelayMessage         // channel → last N msgs
    maxHistory int
}

func NewServer(cfg *Config) *Server {
    signing.InitGroupSignatures()
    return &Server{
        cfg:        cfg,
        clients:    make(map[string]map[*subscriber]struct{}),
        history:    make(map[string][]*pb.RelayMessage),
        maxHistory: 100,
    }
}

// Publish handles one signed message, stamps it, stores it, and broadcasts.
func (s *Server) Publish(
    ctx context.Context,
    msg *pb.RelayMessage,
) (*pb.PublishResponse, error) {
    // ———— 1) Verify the client’s signature ————
    clone := proto.Clone(msg).(*pb.RelayMessage)
    // remove anything not part of the original signature
    clone.Sigma   = nil
    clone.RelayAt = nil

    data, err := proto.MarshalOptions{Deterministic: true}.Marshal(clone)
    if err != nil {
        return nil, status.Errorf(codes.InvalidArgument, "bad encoding: %v", err)
    }
    if !signing.GrpSigVerify(s.cfg.GPK, msg.Sigma, data) {
        return nil, status.Error(codes.Unauthenticated, "invalid signature")
    }

    // ———— 2) Stamp with the server’s time (into relay_at) ————
    now := timestamppb.Now()
    msg.RelayAt = now

    s.mu.Lock()
    // — append to history (ring‐buffer style)
    hist := append(s.history[msg.Channel], msg)
    if len(hist) > s.maxHistory {
        hist = hist[len(hist)-s.maxHistory:]
    }
    s.history[msg.Channel] = hist

    // — broadcast to all except the originator
    for sub := range s.clients[msg.Channel] {
        if sub.senderID == msg.SenderId {
            continue
        }
        go func(sb *subscriber, m *pb.RelayMessage) {
            if err := sb.stream.Send(m); err != nil {
                // clean up dead subscriber
                s.mu.Lock()
                delete(s.clients[m.Channel], sb)
                if len(s.clients[m.Channel]) == 0 {
                    delete(s.clients, m.Channel)
                }
                s.mu.Unlock()
            }
        }(sub, msg)
    }
    s.mu.Unlock()

    return &pb.PublishResponse{RelayAt: now}, nil
}

// Subscribe authenticates, replays history, then serves live messages.
func (s *Server) Subscribe(
    req *pb.SubscribeRequest,
    stream pb.RelayService_SubscribeServer,
) error {
    // ———— 0) Verify subscribe signature ————
    cloneReq := proto.Clone(req).(*pb.SubscribeRequest)
    cloneReq.Sigma = nil
    data, err := proto.MarshalOptions{Deterministic: true}.Marshal(cloneReq)
    if err != nil {
        return status.Errorf(codes.InvalidArgument, "bad subscribe encoding: %v", err)
    }
    if !signing.GrpSigVerify(s.cfg.GPK, req.Sigma, data) {
        return status.Error(codes.Unauthenticated, "invalid subscribe signature")
    }

    // ———— 1) Register the new subscriber ————
    sub := &subscriber{
        stream:   stream,
        ctx:      stream.Context(),
        senderID: req.SenderId,
    }
    s.mu.Lock()
    if s.clients[req.Channel] == nil {
        s.clients[req.Channel] = make(map[*subscriber]struct{})
    }
    s.clients[req.Channel][sub] = struct{}{}

    // snapshot & replay history
    histCopy := append([]*pb.RelayMessage(nil), s.history[req.Channel]...)
    s.mu.Unlock()

    for _, m := range histCopy {
        // skip their own past messages, if desired
        if m.SenderId == sub.senderID {
            continue
        }
        if err := stream.Send(m); err != nil {
            return err
        }
    }

    // ———— 2) Wait for disconnect ————
    <-sub.ctx.Done()

    // ———— 3) Clean up ————
    s.mu.Lock()
    delete(s.clients[req.Channel], sub)
    if len(s.clients[req.Channel]) == 0 {
        delete(s.clients, req.Channel)
    }
    s.mu.Unlock()
    return nil
}
