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

	reqClone := proto.Clone(req).(*pb.EnrollmentRequest)
	reqClone.AuthSigs = nil
	dataBytes, err := proto.Marshal(reqClone)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "error processing request")
	}

	for i := 0; i < len(req.PublicKeys); i++ {
		failed := true
		if pk, err := signing.DecodeString(req.PublicKeys[i]); err == nil {
			if sig, err := signing.DecodeString(req.AuthSigs[i]); err == nil {
				if signing.RegSigVerify(pk, dataBytes, sig) {
					failed = false
				}
			}
		}
		if failed {
			return nil, status.Errorf(codes.InvalidArgument, "failed to prove possesion of public key %d", i)
		}
	}

	// --- Response Step ---
	enrollmentID, err := merkle.CreateRoot([][]string{
		{req.Tn},
		{req.GetIden().Name, req.GetIden().LogoUrl},
		req.PublicKeys,
		req.AuthSigs,
		{fmt.Sprintf("%d", req.NBio)},
		{req.Nonce},
	})

	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate your enrollment record: %v", err)
	}

	expirationTime := time.Now().Add(time.Hour * 24 * time.Duration(s.cfg.EnrollmentDurationDays))
	expirationBytes, _ := expirationTime.MarshalBinary()
	dataToSign := append(enrollmentID, expirationBytes...)
	sigma := signing.RegSigSign(s.cfg.PrivateKey, dataToSign)

	usk, _ := signing.GrpSigUserKeyGen(s.cfg.GPK, s.cfg.ISK)

	response := &pb.EnrollmentResponse{
		Eid:   signing.EncodeToString(enrollmentID),
		Exp:   timestamppb.New(expirationTime),
		Usk:   signing.EncodeToString(usk),
		Sigma: signing.EncodeToString(sigma),
	}

	log.Printf("Successfully processed enrollment for TN: %s. Enrollment ID: %x", req.GetTn(), enrollmentID)
	return response, nil
}
