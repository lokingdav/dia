package enrollment

import (
	"context"
	"fmt"
	"log"
	"time"

	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"github.com/dense-identity/denseid/internal/merkle"
	"github.com/dense-identity/denseid/internal/signing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server implements pb.EnrollmentServiceServer.
type Server struct {
	pb.UnimplementedEnrollmentServiceServer
	cfg *Config
}

// NewServer constructs a new enrollment server.
func NewServer(cfg *Config) *Server {
	signing.InitGroupSignatures()
	return &Server{cfg: cfg}
}

// EnrollSubscriber verifies each RegSig on the request, builds a Merkle root,
// signs it (with expiry), generates a BBS04 user key, and returns all three.
func (s *Server) EnrollSubscriber(ctx context.Context, req *pb.EnrollmentRequest) (*pb.EnrollmentResponse, error) {
	log.Printf("[Enroll] TN=%s, #pubkeys=%d", req.GetTn(), len(req.PublicKeys))

	// 1) Basic length check
	if len(req.PublicKeys) != len(req.AuthSigs) {
		return nil, status.Error(codes.InvalidArgument, "public_keys and auth_sigs count mismatch")
	}

	// 2) Clone & zero out the AuthSigs for deterministic marshaling
	reqClone := proto.Clone(req).(*pb.EnrollmentRequest)
	reqClone.AuthSigs = nil

	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(reqClone)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to marshal request: %v", err)
	}

	// 3) Verify every RegSig
	for i := range req.PublicKeys {
		pkBytes, err := signing.DecodeString(req.PublicKeys[i])
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid public_key[%d]: %v", i, err)
		}
		sigBytes, err := signing.DecodeString(req.AuthSigs[i])
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid auth_sig[%d]: %v", i, err)
		}
		if ok := signing.RegSigVerify(pkBytes, data, sigBytes); !ok {
			return nil, status.Errorf(codes.Unauthenticated, "signature verification failed for key %d", i)
		}
	}

	// 4) Build a Merkle root over all identity fields
	leaves := [][]string{
		{req.Tn},
		{req.GetIden().Name, req.GetIden().LogoUrl},
		req.PublicKeys,
		req.AuthSigs,
		{fmt.Sprintf("%d", req.NBio)},
		{req.Nonce},
	}
	enrollmentID, err := merkle.CreateRoot(leaves)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build Merkle root: %v", err)
	}

	// 5) Compute expiration and sign (enrollmentID || expiry)
	expiry := time.Now().Add(time.Duration(s.cfg.EnrollmentDurationDays) * 24 * time.Hour)
	expiryPb := timestamppb.New(expiry)
	expiryBytes, err := expiry.MarshalBinary()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode expiry: %v", err)
	}

	msgToSign := append(enrollmentID, expiryBytes...)
	enrollmentSig := signing.RegSigSign(s.cfg.PrivateKey, msgToSign)

	// 6) Generate the user secret key (BBS04)
	usk, err := signing.GrpSigUserKeyGen(s.cfg.GPK, s.cfg.ISK)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate user key: %v", err)
	}

	// 7) Build response
	resp := &pb.EnrollmentResponse{
		Eid:   signing.EncodeToString(enrollmentID),
		Exp:   expiryPb,
		Usk:   signing.EncodeToString(usk),
		Sigma: signing.EncodeToString(enrollmentSig),
	}

	log.Printf("[Enroll] Success TN=%s EID=%s", req.GetTn(), resp.Eid)
	return resp, nil
}
