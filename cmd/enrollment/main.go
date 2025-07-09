package main

import (
	"log"
	"net"

	"google.golang.org/grpc"

	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/enrollment"
)

func main() {
	// init pairing based operations for group signatures
	enrollment.InitGroupSignatures()

	cfg, err := config.New[enrollment.Config]()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	cfg.ParseKeysAsBytes()

	// Start listening on cfg.Port
	lis, err := net.Listen("tcp", cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create new enrollment gRPC server and register the service
	s := grpc.NewServer()

	enrollmentServer := enrollment.NewServer(cfg)

	pb.RegisterEnrollmentServiceServer(s, enrollmentServer)

	log.Printf("gRPC server listening at %v", lis.Addr())

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
