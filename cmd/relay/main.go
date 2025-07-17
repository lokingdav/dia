package main

import (
	"log"
	"net"
	"strings"

	"google.golang.org/grpc"

	relaypb "github.com/dense-identity/denseid/api/go/relay/v1"
	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/relay"
)

func main() {
	// 1) Load your custom relay.Config via the generic loader
	cfg, err := config.New[relay.Config]()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	// 2) Parse any key‐material out of the loaded strings
	err = cfg.ParseKeysAsBytes()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// 3) Make sure the port has a leading “:”
	//    (e.g. if cfg.Port == "50051", this becomes ":50051")
	addr := cfg.Port
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	// 4) Start listening
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", addr, err)
	}

	// 5) Create gRPC server and register your relay service
	grpcServer := grpc.NewServer()
	relayServer := relay.NewServer(cfg)
	relaypb.RegisterRelayServiceServer(grpcServer, relayServer)

	log.Printf("gRPC server listening at %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC serve error: %v", err)
	}
}
