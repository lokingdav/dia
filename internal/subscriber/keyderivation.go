package subscriber

import (
	"log"

	keyderivationpb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
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

func RunKeyDerivation(callerId string) {
	
}