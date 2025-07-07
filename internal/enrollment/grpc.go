package enrollment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server implements the pb.EnrollmentServiceServer interface.
type Server struct {
	pb.UnimplementedEnrollmentServiceServer
	cfg *Config
}

// NewServer creates a new instance of our enrollment server.
func NewServer(cfg *Config) *Server {
	return &Server{
		cfg: cfg,
	}
}

// EnrollSubscriber implements the RPC method for enrolling a new subscriber.
func (s *Server) EnrollSubscriber(ctx context.Context, req *pb.EnrollmentRequest) (*pb.EnrollmentResponse, error) {
	log.Printf("Received enrollment request for TN: %s", req.GetTn())

	// --- Validation Step ---
	if req.GetTn() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "telephone number (tn) is a required field")
	}

	log.Printf("Request: %v", req)
	
	// --- Response Step ---
	enrollmentID := uuid.New().String()
	expirationTime := time.Now().Add(time.Hour * 24 * 90)
	usk, _ := generateRandomHex(32)
	sig1, _ := generateRandomHex(64)
	sig2, _ := generateRandomHex(64)

	response := &pb.EnrollmentResponse{
		Eid:  enrollmentID,
		Exp:  timestamppb.New(expirationTime),
		Usk:  usk,
		Signatures: &pb.Signatures{
			Sig1: sig1,
			Sig2: sig2,
		},
	}

	log.Printf("Successfully processed enrollment for TN: %s. Enrollment ID: %s", req.GetTn(), enrollmentID)
	return response, nil
}

// generateRandomHex is a helper function to create a random hex string.
func generateRandomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("could not generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}