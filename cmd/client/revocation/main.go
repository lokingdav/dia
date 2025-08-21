package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"

	revocationpb "github.com/dense-identity/denseid/api/go/revocation/v1"
	configpkg "github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/signing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// clientConfig holds our environment-loaded group keys.
type clientConfig struct {
	GPKStr string `env:"GROUP_PK,required"`
	USKStr string `env:"GROUP_USK,required"`
	GPK    []byte
	USK    []byte
}

func (c *clientConfig) ParseKeysAsBytes() error {
	if c == nil {
		return errors.New("config is nil")
	}
	var err error
	if c.GPK, err = signing.DecodeHex(c.GPKStr); err != nil {
		return fmt.Errorf("decoding GROUP_PK: %w", err)
	}
	if c.USK, err = signing.DecodeHex(c.USKStr); err != nil {
		return fmt.Errorf("decoding GROUP_USK: %w", err)
	}
	return nil
}

func loadConfig() *clientConfig {
	if err := configpkg.LoadEnv(".env.client"); err != nil {
		log.Printf("warning loading .env.client: %v", err)
	}
	cfg, err := configpkg.New[clientConfig]()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	if err := cfg.ParseKeysAsBytes(); err != nil {
		log.Fatalf("parsing keys: %v", err)
	}
	return cfg
}

func createGRPCClient(addr string) revocationpb.RevocationServiceClient {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("grpc.NewClient(%q): %v", addr, err)
	}
	return revocationpb.NewRevocationServiceClient(conn)
}

func main() {
	// Command-line flags
	serverAddr := flag.String("server", "localhost:50055", "revocation server address")
	queryMsg := flag.String("query", "HelloWorld", "Query message to check")
	flag.Parse()

	// Load config and init signing
	cfg := loadConfig()
	fmt.Printf("cfg: %v", cfg)
	// signing.InitGroupSignatures()

	// Create gRPC client
	client := createGRPCClient(*serverAddr)

	req := &revocationpb.QueryRequest{
		Query: []byte(*queryMsg),
	}

	// Sign request
	cloneReq := proto.Clone(req).(*revocationpb.QueryRequest)
	cloneReq.Sigma = nil
	// data, err := proto.MarshalOptions{Deterministic: true}.Marshal(cloneReq)
	// if err != nil {
	// 	log.Fatalf("marshal request: %v", err)
	// }
	// sig, err := signing.GrpSigSign(cfg.GPK, cfg.USK, data)
	// if err != nil {
	// 	log.Fatalf("sign request: %v", err)
	// }
	// req.Sigma = sig

	// Call Query
	response, err := client.Query(context.Background(), req)
	if err != nil {
		log.Fatalf("Query RPC error: %v", err)
	}

	// Print result
	fmt.Printf("Is Revoked: '%v'\n", response.IsRevoked)
}
