package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	keyderivationpb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
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
	if c.GPK, err = signing.DecodeString(c.GPKStr); err != nil {
		return fmt.Errorf("decoding GROUP_PK: %w", err)
	}
	if c.USK, err = signing.DecodeString(c.USKStr); err != nil {
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

func createGRPCClient(addr string) keyderivationpb.KeyDerivationServiceClient {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("grpc.NewClient(%q): %v", addr, err)
	}
	return keyderivationpb.NewKeyDerivationServiceClient(conn)
}

func main() {
	// Command-line flags
	serverAddr := flag.String("server", "localhost:50053", "key-derivation server address")
	flag.Parse()

	// Load config and init signing
	cfg := loadConfig()
	signing.InitGroupSignatures()

	// Create gRPC client
	client := createGRPCClient(*serverAddr)

	// Prompt user for blinded element
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter blinded element (hex): ")
	line, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("reading input: %v", err)
	}
	hexStr := strings.TrimSpace(line)
	blinded, err := hex.DecodeString(hexStr)
	if err != nil {
		log.Fatalf("invalid hex: %v", err)
	}

	// Build request
	req := &keyderivationpb.EvaluateRequest{
		BlindedElement: blinded,
	}
	// Sign request
	cloneReq := proto.Clone(req).(*keyderivationpb.EvaluateRequest)
	cloneReq.Sigma = nil
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(cloneReq)
	if err != nil {
		log.Fatalf("marshal request: %v", err)
	}
	sig, err := signing.GrpSigSign(cfg.GPK, cfg.USK, data)
	if err != nil {
		log.Fatalf("sign request: %v", err)
	}
	req.Sigma = sig

	// Call Evaluate
	resp, err := client.Evaluate(context.Background(), req)
	if err != nil {
		log.Fatalf("Evaluate RPC error: %v", err)
	}

	// Print result
	fmt.Printf("Evaluated element (hex): %s\n", hex.EncodeToString(resp.EvaluatedElement))
}
