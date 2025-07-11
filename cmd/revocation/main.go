package main

import (
	"log"
	"net"
	"strings"

	"google.golang.org/grpc"

	revocationpb "github.com/dense-identity/denseid/api/go/revocation/v1"
	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/revocation"
)

func main() {
	// Load service configuration (including Port, GPK)
	cfg, err := config.New[revocation.Config]()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	// Parse keys from their encoded string formats
	if err := cfg.ParseKeysAsBytes(); err != nil {
		log.Fatalf("failed to parse keys: %v", err)
	}

	// Ensure port has leading ':'
	addr := cfg.Port
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	// Listen on the configured address
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", addr, err)
	}

	// Create gRPC server and register the RevocationService
	grpcServer := grpc.NewServer()
	service := revocation.NewServer(cfg)
	revocationpb.RegisterRevocationServiceServer(grpcServer, service)

	log.Printf("RevocationService listening at %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("service error: %v", err)
	}
}
