package main

import (
	"log"
	"net"

	"google.golang.org/grpc"

	pb "github.com/dense-identity/denseid/api/go/relay/v1"
	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/relay"
)

func main() {
	cfg, err := config.New[relay.Config]()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	cfg.ParseKeysAsBytes()

	// Start listening on cfg.Port
	lis, err := net.Listen("tcp", cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create new relay gRPC server and register the service
	s := grpc.NewServer()

	relayServer := relay.NewServer(cfg)

	pb.RegisterRelayServiceServer(s, relayServer)

	log.Printf("gRPC server listening at %v", lis.Addr())

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
