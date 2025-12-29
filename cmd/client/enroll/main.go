package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	mr "math/rand/v2"

	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	dia "github.com/lokingdav/libdia/bindings/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func createGRPCClient(addr string) pb.EnrollmentServiceClient {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("grpc.NewClient(%q): %v", addr, err)
	}
	return pb.NewEnrollmentServiceClient(conn)
}

func main() {
	serverAddr := flag.String("host", "localhost:50051", "Server address")
	phone := flag.String("phone", "", "Phone number to be enrolled with")
	name := flag.String("name", "", "Name of subscriber")
	logoUrl := flag.String("logo", "", "Logo url")
	flag.Parse()

	if *name == "" {
		log.Fatal("--name is required")
	}
	if *phone == "" {
		log.Fatal("--phone is required")
	}
	if *logoUrl == "" {
		*logoUrl = fmt.Sprintf("https://avatar.iran.liara.run/public/%d", mr.IntN(55))
	}

	// Create DIA enrollment request (handles all crypto internally)
	keys, diaRequest, err := dia.CreateEnrollmentRequest(*phone, *name, *logoUrl, 1)
	if err != nil {
		log.Fatalf("Error creating enrollment request: %v", err)
	}

	// Wrap DIA request in protobuf for gRPC transport
	req := &pb.EnrollmentRequest{
		DiaRequest: diaRequest,
	}

	// Call gRPC enrollment endpoint
	client := createGRPCClient(*serverAddr)
	res, err := client.EnrollSubscriber(context.Background(), req)
	if err != nil {
		log.Fatalf("Error enrolling subscriber: %v", err)
	}

	// Finalize enrollment with DIA response
	config, err := dia.FinalizeEnrollment(keys, res.DiaResponse, *phone, *name, *logoUrl)
	if err != nil {
		log.Fatalf("Error finalizing enrollment: %v", err)
	}

	// Output client configuration in DIA format
	envString, err := config.ToEnv()
	if err != nil {
		log.Fatalf("Error serializing config: %v", err)
	}

	fmt.Println("# DIA Client Configuration")
	fmt.Println("# Save this securely - it contains your enrollment credentials")
	fmt.Println()
	fmt.Println(envString)
}
