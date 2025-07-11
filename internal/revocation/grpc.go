package revocation

import (
	"context"

	revocationpb "github.com/dense-identity/denseid/api/go/revocation/v1"
	"github.com/dense-identity/denseid/internal/signing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Server implements the RevocationServiceServer interface.
type Server struct {
	revocationpb.UnimplementedRevocationServiceServer
	cfg *Config
}

// NewServer creates a new RevocationService server.
func NewServer(cfg *Config) *Server {
	// Initialize pairing for BBS04 group signatures
	signing.InitGroupSignatures()
	return &Server{cfg: cfg}
}

// Query verifies the group signature on the blinded element
// and performs the revocation lookup.
func (s *Server) Query(
	ctx context.Context,
	req *revocationpb.QueryRequest,
) (*revocationpb.QueryResponse, error) {
	// 1) Verify group signature over the blinded element
	clone := proto.Clone(req).(*revocationpb.QueryRequest)
	clone.Sigma = nil
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(clone)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "bad request encoding: %v", err)
	}
	if !signing.GrpSigVerify(s.cfg.GPK, req.Sigma, data) {
		return nil, status.Error(codes.Unauthenticated, "invalid signature")
	}

	// process revocation

	return &revocationpb.QueryResponse{
		IsRevoked: false,
	}, nil
}
