package enrollment

import (
	"context"
	"fmt"
	"log"

	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/merkle"
	"github.com/dense-identity/denseid/internal/signing"
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
	// signing.InitGroupSignatures()
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
		clientPk, err := signing.ImportPublicKeyFromDER(req.PublicKeys[i])

		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "public key malformed. DER expected")
		}

		if ok := signing.RegSigVerify(clientPk, data, req.AuthSigs[i]); !ok {
			return nil, status.Errorf(codes.Unauthenticated, "signature verification failed for key %d", i)
		}
	}

	// 4) Build a Merkle root over all enrollment records
	leaves := []string{
		req.Tn, // Telephone number
		req.GetIden().Name,
		req.GetIden().LogoUrl,       // Display information related
		fmt.Sprintf("%d", req.NBio), // biometric count
		req.Nonce,                   // Nonce
	}
	for i := 0; i < len(req.PublicKeys); i++ {
		leaves = append(leaves, signing.EncodeToHex(req.PublicKeys[i]))
	}

	enrollmentID, err := merkle.CreateRoot(leaves)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build Merkle root: %v", err)
	}

	// 5) Compute expiration and sign (enrollmentID || expiry)
	expiryPb := datetime.MakeExpiration(s.cfg.EnrollmentDurationDays)
	eid := signing.EncodeToHex(enrollmentID)

	msgToSign, err := proto.MarshalOptions{Deterministic: true}.Marshal(&pb.EnrollmentResponse{
		Eid: eid,
		Exp: expiryPb,
	})
	if err != nil {
		status.Errorf(codes.Internal, "failed to sign enrollment: %v", err)
	}

	enrollmentSig := signing.RegSigSign(s.cfg.PrivateKey, msgToSign)

	// 6) Generate the user secret key (BBS04)
	log.Printf("Running USK KEYGEN\nGPK=%x\nISK=%x", s.cfg.GPK, s.cfg.ISK)
	// usk, err := signing.GrpSigUserKeyGen(s.cfg.GPK, s.cfg.ISK)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate user key: %v", err)
	}

	// 7) Build response
	resp := &pb.EnrollmentResponse{
		Eid:       eid,
		Exp:       expiryPb,
		// Usk:       usk,
		Gpk:       s.cfg.GPK,
		Sigma:     enrollmentSig,
		PublicKey: s.cfg.PublicKeyDER,
	}

	log.Printf("[Enroll] Success TN=%s EID=%s", req.GetTn(), resp.Eid)
	return resp, nil
}
