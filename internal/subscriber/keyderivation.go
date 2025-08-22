package subscriber

import (
	"context"
	"log"

	keyderivationpb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	"github.com/dense-identity/denseid/internal/voprf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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

func RunKeyDerivation(callerId, ts string, ticket []byte, serverAddr string) ([]byte, error) {
	blinded, blind, err := voprf.Blind([]byte(callerId + ts))
	if err != nil {
		return nil, err
	}
	
	request := &keyderivationpb.EvaluateRequest{
		BlindedElement: blinded,
		Ticket: ticket,
	}
	
	client := createGRPCClient(serverAddr)
	response, err := client.Evaluate(context.Background(), request)
	if err != nil {
		log.Fatalf("Error Evaluating input: %v", err)
	}

	return voprf.Finalize(response.GetEvaluatedElement(), blind)
}