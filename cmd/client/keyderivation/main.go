package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"

	keyderivationpb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	configpkg "github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/signing"
	"github.com/dense-identity/denseid/internal/voprf"
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

func runOPRF(client keyderivationpb.KeyDerivationServiceClient, inputMsg, GPK, USK []byte) (string, error) {
	blind, blindedElement, err := voprf.Blind(inputMsg)

	if err != nil {
		return "", fmt.Errorf("error blinding input message: %v", err)
	}

	// Build request
	req := &keyderivationpb.EvaluateRequest{
		BlindedElement: blindedElement,
	}

	// Sign request
	cloneReq := proto.Clone(req).(*keyderivationpb.EvaluateRequest)
	cloneReq.Sigma = nil
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(cloneReq)
	if err != nil {
		return "", fmt.Errorf("marshal request: %v", err)
	}
	sig, err := signing.GrpSigSign(GPK, USK, data)
	if err != nil {
		return "", fmt.Errorf("sign request: %v", err)
	}
	req.Sigma = sig

	// Call Evaluate
	response, err := client.Evaluate(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("evaluate RPC error: %v", err)
	}

	// Finalize the result
	result, err := voprf.Finalize(blind, response.EvaluatedElement)
	if err != nil {
		return "", fmt.Errorf("error finalizing: %v", err)
	}

	return signing.EncodeToHex(result), nil
}

func main() {
	// Command-line flags
	serverAddr := flag.String("server", "localhost:50053", "key-derivation server address")
	inputMsg := flag.String("input", "Hello World", "Input message to evaluate")
	flag.Parse()

	// Load config and init signing
	cfg := loadConfig()
	signing.InitGroupSignatures()

	// Create gRPC client
	client := createGRPCClient(*serverAddr)
	input := []byte(*inputMsg)

	control, err := runOPRF(client, []byte("Hello World!!!"), cfg.GPK, cfg.USK)
	if err != nil {
		log.Fatalf("fail to compute control: %v", err)
	}

	result1, err := runOPRF(client, input, cfg.GPK, cfg.USK)
	if err != nil {
		log.Fatalf("fail to compute result 1: %v", err)
	}

	result2, err := runOPRF(client, input, cfg.GPK, cfg.USK)
	if err != nil {
		log.Fatalf("fail to compute result 2: %v", err)
	}

	if control == result1 || control == result2 {
		log.Fatal("Wrong OPRF results: 'control' matches 'result1' or 'result2' but shouldn't.")
	}

	if result1 != result2 {
		log.Fatalf("Wrong OPRF results: 'result1' must match 'result2'\nresult1 = %s\nresult2 = %s", result1, result2)
	}

	// Print result
	fmt.Printf("Input Msg: '%s', Evaluated Result: '%s'\n", *inputMsg, result1)
}
