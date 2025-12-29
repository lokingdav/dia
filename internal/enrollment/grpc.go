package enrollment

import (
	"context"
	"log"

	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements pb.EnrollmentServiceServer.
type Server struct {
	pb.UnimplementedEnrollmentServiceServer
	cfg *Config
}

// NewServer constructs a new enrollment server.
func NewServer(cfg *Config) *Server {
	return &Server{cfg: cfg}
}

// EnrollSubscriber processes an enrollment request using the DIA library.
// All cryptographic operations (verification, signing, ticket generation) are
// handled internally by the DIA server configuration.
func (s *Server) EnrollSubscriber(ctx context.Context, req *pb.EnrollmentRequest) (*pb.EnrollmentResponse, error) {
	log.Printf("[Enroll] Processing DIA enrollment request")

	if s.cfg.DiaServerConfig == nil {
		return nil, status.Error(codes.Internal, "DIA server config not initialized")
	}

	// Process enrollment using DIA library (handles all crypto)
	diaResponse, err := s.cfg.DiaServerConfig.ProcessEnrollment(req.DiaRequest)
	if err != nil {
		log.Printf("[Enroll] Failed: %v", err)
		return nil, status.Errorf(codes.Internal, "enrollment processing failed: %v", err)
	}

	resp := &pb.EnrollmentResponse{
		DiaResponse: diaResponse,
	}

	log.Printf("[Enroll] Success")
	return resp, nil
}
