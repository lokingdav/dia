package keyderivation

import (
	"context"
	"log"

	keyderivationpb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	// "github.com/dense-identity/denseid/internal/signing"
	"github.com/dense-identity/denseid/internal/voprf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the KeyDerivationServiceServer interface.
type Server struct {
	keyderivationpb.UnimplementedKeyDerivationServiceServer
	cfg *Config
}

// NewServer creates a new KeyDerivationService server.
func NewServer(cfg *Config) *Server {
	// Initialize pairing for BBS04 group signatures
	// signing.InitGroupSignatures()
	return &Server{cfg: cfg}
}

// Evaluate verifies the group signature on the blinded element
// and performs the OPRF evaluation.
func (s *Server) Evaluate(
	ctx context.Context,
	req *keyderivationpb.EvaluateRequest,
) (*keyderivationpb.EvaluateResponse, error) {
	log.Printf("[Evaluate] blindedElement=%x", req.BlindedElement)

	// Verify ticket
	ticketIsValid, err := voprf.VerifyTicket(req.Ticket, s.cfg.AtVerifyKey)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unauthorized: %v", err)
	}
	if !ticketIsValid {
		return nil, status.Error(codes.InvalidArgument, "Invalid ticket")
	}

	// Perform the OPRF evaluation
	evaluated, err := voprf.Evaluate(s.cfg.KsPrivateKey, req.BlindedElement)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "OPRF evaluation failed: %v", err)
	}

	return &keyderivationpb.EvaluateResponse{
		EvaluatedElement: evaluated,
	}, nil
}
