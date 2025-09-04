package relay

import (
	pb "github.com/dense-identity/denseid/api/go/relay/v1"
)

// Server implements RelayServiceServer.
type Server struct {
	pb.UnimplementedRelayServiceServer

	cfg          *Config
	streamServer *StreamServer
}

func NewServer(cfg *Config) (*Server, error) {
	// Create the new streaming server
	streamServer, err := NewStreamServer(cfg)
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg:          cfg,
		streamServer: streamServer,
	}, nil
}

// Close gracefully shuts down the server
func (s *Server) Close() error {
	if s.streamServer != nil {
		return s.streamServer.Close()
	}
	return nil
}

// Relay implements the new bidirectional streaming RPC
func (s *Server) Relay(stream pb.RelayService_RelayServer) error {
	return s.streamServer.Relay(stream)
}
