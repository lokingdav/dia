package main

import (
	"log"
	"net"
	"strings"

	"google.golang.org/grpc"

	keyderivationpb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/keyderivation"
	"github.com/dense-identity/denseid/internal/signing"
)

func main() {
	// Load service configuration (including Port, GPK, USK)
	cfg, err := config.New[keyderivation.Config]()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	// Parse group keys from their encoded string formats
	if err := cfg.ParseKeysAsBytes(); err != nil {
		log.Fatalf("failed to parse group keys: %v", err)
	}

	// Initialize BBS04 group signature pairing parameters
	signing.InitGroupSignatures()

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

	// Create gRPC server and register the KeyDerivationService
	grpcServer := grpc.NewServer()
	service := keyderivation.NewServer(cfg)
	keyderivationpb.RegisterKeyDerivationServiceServer(grpcServer, service)

	log.Printf("KeyDerivationService listening at %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("service error: %v", err)
	}
}
