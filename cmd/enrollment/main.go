package main

import (
	"log"
	"net"

	"google.golang.org/grpc"

	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"github.com/dense-identity/denseid/internal/enrollment"
)

func main() {
	port := ":50051"
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()

	enrollmentServer := enrollment.NewServer()

	pb.RegisterEnrollmentServiceServer(s, enrollmentServer)

	log.Printf("gRPC server listening at %v", lis.Addr())

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}