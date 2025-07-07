package main

import (
	"context"
	"log"
	"time"

	// Import the generated protobuf code.
	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	serverAddress = "localhost:50051"
)

func main() {
	conn, err := grpc.NewClient(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewEnrollmentServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	log.Println("--- Calling EnrollSubscriber ---")

	request := &pb.EnrollmentRequest{
		Tn:          "+15551234567",
		PublicKeys:  []string{"pubkey1_hex_value", "pubkey2_hex_value"},
		AuthSigs:        []string{"sig1_hex_value", "sig2_hex_value"},
		NBio:          2,
		Iden: &pb.DisplayInformation{
			Name:       "John Doe Inc.",
			LogoUrl:    "https://example.com/logo.png",
			WebsiteUrl: "https://johndoe.com",
			BrandColor: "#0A192F",
			Tagline:    "Building the future.",
		},
	}

	response, err := client.EnrollSubscriber(ctx, request)
	if err != nil {
		log.Fatalf("could not enroll subscriber: %v", err)
	}

	log.Printf("Enrollment Successful!")
	log.Printf("  Enrollment ID (eid): %s", response.GetEid())
	log.Printf("  User Secret Key (usk): %s", response.GetUsk())
	log.Printf("  Expiration (exp): %s", response.GetExp().AsTime())
	log.Printf("  Signature 1 (sig1): %s", response.GetSignatures().GetSig1())
	log.Printf("  Signature 2 (sig2): %s", response.GetSignatures().GetSig2())
}
