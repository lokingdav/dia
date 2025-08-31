package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/protocol"
	"github.com/dense-identity/denseid/internal/subscriber"
)

func createCallState() *protocol.CallState {
	phone := flag.String("phone", "", "Phone number to dial/recieve calls to/from")
	action := flag.String("action", "dial", "Either dial/receive")
	envfile := flag.String("env", "", ".env file which contains details of the subscriber")
	flag.Parse()

	if *phone == "" {
		log.Fatal("--phone is required")
	}
	if *action == "" {
		log.Fatal("--action is required to be either dial or receive")
	}
	if *envfile == "" {
		log.Fatal("--env is required for subscriber authentication")
	}

	if err := config.LoadEnv(*envfile); err != nil {
		log.Printf("warning loading %s: %v", *envfile, err)
	}

	config, err := config.New[config.SubscriberConfig]()
	if err != nil {
		log.Fatalf("failed to load subscriber config: %v", err)
	}
	err = config.ParseKeysAsBytes()
	if err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}

	state := protocol.CreateCallState(config, *phone, *action)

	return &state
}

func KeyDerivation(ctx context.Context, callState *protocol.CallState) {
	kdClient, err := subscriber.NewKeyDeriveClient(callState.Config.KeyServerAddr)
	if err != nil {
		log.Fatalf("error creating key derivation client: %v", err)
	}
	sharedKey, err := protocol.AkeDeriveKey(ctx, kdClient.Stub, callState)
	if err != nil {
		log.Fatalf("error deriving key: %v", err)
	}
	err = kdClient.Close()
	if err != nil {
		log.Fatalf("unable to close keyderivation client: %v", err)
	}
	callState.SetSharedKey(sharedKey)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	
	// Initialize call state
	callState := createCallState()

	// Derive shared key and update state
	KeyDerivation(ctx, callState)

	log.Printf("Shared Key: %x", callState.SharedKey)

	// oobController, err := subscriber.NewController(callState)
	// if err != nil {
	// 	panic(err)
	// }

	// // Start receiving (non-blocking)
	// oobController.Start(func(b []byte) {
	// 	fmt.Println("RX:", string(b))
	// })

	// if callState.IsCaller { // send initial auth info
	// 	if err := oobController.Send([]byte("AUTH_INIT|from=+15551234567")); err != nil {
	// 		println("publish failed:", err.Error())
	// 	}
	// }

	// // Wait for Ctrl-C, then close gracefully
	// // <-ctx.Done()

	// _ = oobController.Close()
}
