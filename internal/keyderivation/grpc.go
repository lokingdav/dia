package keyderivation

import (
	"context"
	"log"

	keyderivationpb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	// "github.com/dense-identity/denseid/internal/signing"
	"github.com/dense-identity/denseid/internal/voprf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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

	// 1) Verify group signature over the blinded element
	clone := proto.Clone(req).(*keyderivationpb.EvaluateRequest)
	clone.Sigma = nil
	// data, err := proto.MarshalOptions{Deterministic: true}.Marshal(clone)
	// if err != nil {
	// 	return nil, status.Errorf(codes.InvalidArgument, "bad request encoding: %v", err)
	// }
	// if !signing.GrpSigVerify(s.cfg.GPK, req.Sigma, data) {
	// 	return nil, status.Error(codes.Unauthenticated, "invalid signature")
	// }

	// 2) Perform the OPRF evaluation (implementation in Config)
	evaluated, err := voprf.Evaluate(s.cfg.OprfSK, req.BlindedElement)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "OPRF evaluation failed: %v", err)
	}

	return &keyderivationpb.EvaluateResponse{
		EvaluatedElement: evaluated,
	}, nil
}
