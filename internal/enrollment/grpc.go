package enrollment

import (
	"context"
	"crypto/rand"
	"log"

	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/signing"
	"github.com/dense-identity/denseid/internal/voprf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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

// EnrollSubscriber verifies each RegSig on the request, builds a Merkle root,
// signs it (with expiry), generates a BBS04 user key, and returns all three.
func (s *Server) EnrollSubscriber(ctx context.Context, req *pb.EnrollmentRequest) (*pb.EnrollmentResponse, error) {
	log.Printf("[Enroll] TN=%s, Iden=%v", req.GetTn(), req.GetIden())

	// Verify request signature
	reqClone := proto.Clone(req).(*pb.EnrollmentRequest)
	reqClone.Sigma = nil
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(reqClone)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to marshal request: %v", err)
	}
	clientIpk, err := signing.ImportPublicKeyFromDER(req.GetIpk())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "failed to decode ipk")
	}
	if ok := signing.RegSigVerify(clientIpk, data, req.Sigma); !ok {
		return nil, status.Error(codes.Unauthenticated, "signature verification failed")
	}

	// Generate enrollment ID and expiration
	reid := make([]byte, 32)
	_, err = rand.Read(reid)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate eid: %v", err)
	}
	eid := signing.EncodeToHex(reid)

	expiryPb := datetime.MakeExpiration(s.cfg.EnrollmentDurationDays)
	expiryBytes, err := proto.Marshal(expiryPb)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate expiry: %v", err)
	}

	// Set attributes and sign
	attributes := []string{
		eid,
		signing.EncodeToHex(expiryBytes),
		req.GetIden().Name,
		req.GetIden().LogoUrl,
		signing.EncodeToHex(req.GetPk()),
		signing.EncodeToHex(req.GetIpk()),
		req.Nonce,
	}
	for i, v := range attributes {
		attributes[i] = v + req.Tn
	}

	Sigma, err := signing.BbsSign(s.cfg.CiPrivateKey, attributes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate credential: %v", err)
	}

	evaluatedTickets, err := voprf.BulkEvaluate(s.cfg.AtPrivateKey, req.BlindedTickets)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate tickets: %v", err)
	}

	resp := &pb.EnrollmentResponse{
		Eid:              eid,
		Exp:              expiryPb,
		Epk:              s.cfg.CiPublicKey,
		Sigma:            Sigma,
		Mpk:              s.cfg.AmfPublicKey,
		Avk:              s.cfg.AtPublicKey,
		EvaluatedTickets: evaluatedTickets,
	}

	log.Printf("[Enroll] Success TN=%s EID=%s", req.GetTn(), resp.Eid)
	return resp, nil
}
