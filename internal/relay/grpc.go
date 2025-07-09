package relay

import (
    "io"
    "sync"

    pb "github.com/dense-identity/denseid/api/go/relay/v1"
    "github.com/dense-identity/denseid/internal/signing"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/timestamppb"
)

// Server implements the pb.RelayServiceServer interface.
type Server struct {
    pb.UnimplementedRelayServiceServer
    cfg     *Config
    mu      sync.RWMutex
    clients map[string]map[pb.RelayService_SubscribeServer]struct{}
}

// NewServer creates a new instance of our relay server.
func NewServer(cfg *Config) *Server {
    signing.InitGroupSignatures()
    return &Server{
        cfg:     cfg,
        clients: make(map[string]map[pb.RelayService_SubscribeServer]struct{}),
    }
}

// Subscribe is the bidi‐streaming RPC: clients send RelayMessage and receive RelayMessage.
func (s *Server) Subscribe(stream pb.RelayService_SubscribeServer) error {
    for {
        // 1) receive the next client message
        msg, err := stream.Recv()
        if err == io.EOF {
            // client closed
            return nil
        }
        if err != nil {
            return err
        }

        // 2) verify BBS04 signature over (id, channel, payload, sent_at)
        clone := proto.Clone(msg).(*pb.RelayMessage)
        clone.Sigma = nil
        data, err := proto.MarshalOptions{Deterministic: true}.Marshal(clone)
        if err != nil {
            return status.Errorf(codes.InvalidArgument, "bad message encoding: %v", err)
        }
        if !signing.GrpSigVerify(s.cfg.GPK, msg.Sigma, data) {
            return status.Error(codes.Unauthenticated, "invalid signature")
        }

        // 3) register this stream under its channel (idempotent)
        s.mu.Lock()
        if s.clients[msg.Channel] == nil {
            s.clients[msg.Channel] = make(map[pb.RelayService_SubscribeServer]struct{})
        }
        s.clients[msg.Channel][stream] = struct{}{}
        s.mu.Unlock()

        // 4) stamp the message
        msg.SentAt = timestamppb.Now()

        // 5) broadcast to everyone on that channel
        s.mu.RLock()
        for cli := range s.clients[msg.Channel] {
            go func(c pb.RelayService_SubscribeServer, m *pb.RelayMessage) {
                if serr := c.Send(m); serr != nil {
                    // on error, clean up that dead stream
                    s.mu.Lock()
                    delete(s.clients[m.Channel], c)
                    if len(s.clients[m.Channel]) == 0 {
                        delete(s.clients, m.Channel)
                    }
                    s.mu.Unlock()
                }
            }(cli, msg)
        }
        s.mu.RUnlock()
    }
}
