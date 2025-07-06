package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"time"

	pb "github.com/dense-identity/denseid/api/proto/enrollment"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// server is used to implement the pb.EnrollmentServiceServer interface.
type server struct {
	pb.UnimplementedEnrollmentServiceServer
}

// EnrollSubscriber implements the RPC method for enrolling a new subscriber.
func (s *server) EnrollSubscriber(ctx context.Context, req *pb.EnrollmentRequest) (*pb.EnrollmentResponse, error) {
	log.Printf("Received enrollment request for TN: %s", req.GetTn())

	// --- Validation Step ---
	if req.GetTn() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "telephone number (tn) is a required field")
	}
	if req.GetIden() == nil || req.GetIden().GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "identity information (iden) and name are required fields")
	}
	if len(req.GetPublicKeys()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "at least one public key must be provided")
	}
	if len(req.GetPublicKeys()) != len(req.GetSigs()) {
		return nil, status.Errorf(codes.InvalidArgument, "the number of signatures must match the number of public keys")
	}

	log.Printf("Validation passed for TN: %s", req.GetTn())

	// --- Processing Step ---
	// In a real application, you would perform cryptographic operations,
	// interact with a database, and handle other business logic here.

	// 1. Generate a unique enrollment ID (eid).
	enrollmentID := uuid.New().String()

	// 2. Set an expiration timestamp (exp), e.g., 90 days from now.
	expirationTime := time.Now().Add(time.Hour * 24 * 90)

	// 3. Generate a user secret key (usk).
	// This is a placeholder; use a cryptographically secure method in production.
	usk, err := generateRandomHex(32)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate user secret key: %v", err)
	}

	// 4. Generate response signatures (sigs).
	// These are placeholders. Your actual signature logic would go here.
	sig1, _ := generateRandomHex(64)
	sig2, _ := generateRandomHex(64)

	// --- Response Step ---
	// Construct and return the response message.
	response := &pb.EnrollmentResponse{
		Eid:  enrollmentID,
		Exp:  timestamppb.New(expirationTime),
		Usk:  usk,
		Sigs: &pb.Signatures{
			Sig1: sig1,
			Sig2: sig2,
		},
	}

	log.Printf("Successfully processed enrollment for TN: %s. Enrollment ID: %s", req.GetTn(), enrollmentID)
	return response, nil
}

// main starts the gRPC server.
func main() {
	// Define the port the server will listen on.
	port := ":50051"
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create a new gRPC server instance.
	s := grpc.NewServer()

	// Register our server implementation with the gRPC server.
	pb.RegisterEnrollmentServiceServer(s, &server{})

	log.Printf("gRPC server listening at %v", lis.Addr())

	// Start listening for incoming connections.
	// This is a blocking call.
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// generateRandomHex is a helper function to create a random hex string.
func generateRandomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("could not generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
