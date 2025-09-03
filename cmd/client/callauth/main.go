package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/protocol"
	"github.com/dense-identity/denseid/internal/subscriber"
)

func createCallState() *protocol.CallState {
	dial := flag.String("dial", "", "The phone number to dial")
	receive := flag.String("receive", "", "The phone number to recieve call from")
	envfile := flag.String("env", "", ".env file which contains details of the subscriber")
	flag.Parse()

	if *dial == "" && *receive == "" {
		log.Fatal("--dial or --receive option is required")
	}

	if *dial != "" && *receive != "" {
		log.Fatal("You cannot specify both --dial or --receive. Make your mind up.")
	}

	if *envfile == "" {
		log.Fatal("--env is required for subscriber authentication")
	}

	if err := config.LoadEnv(*envfile); err != nil {
		log.Printf("warning loading %s: %v", *envfile, err)
	}

	var outgoing bool
	var phoneNumber string

	if *dial == "" {
		outgoing = false
		phoneNumber = *receive
	} else {
		outgoing = true
		phoneNumber = *dial
	}

	config, err := config.New[config.SubscriberConfig]()
	if err != nil {
		log.Fatalf("failed to load subscriber config: %v", err)
	}
	err = config.ParseKeysAsBytes()
	if err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}

	state := protocol.NewCallState(config, phoneNumber, outgoing)

	fmt.Println("===== Call Details =====")
	fmt.Printf("--> Outgoing: %v\n", outgoing)
	fmt.Printf("--> CallerID: %s\n", state.CallerId)
	fmt.Printf("--> Recipient: %s\n\n", state.Recipient)

	return &state
}

func RunKeyDerivation(ctx context.Context, callState *protocol.CallState) {
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

func akeRound1(oob *subscriber.Controller, callState *protocol.CallState) {
	err := protocol.InitAke(callState)
	if err != nil {
		log.Fatalf("failed to init AKE: %v", err)
	}

	if callState.IsOutgoing {
		ciphertext, err := protocol.AkeRound1CallerToRecipient(callState)

		if err != nil {
			log.Fatalf("failed creating M1 Caller --> Recipient: %v", err)
		}

		if err := oob.Send(ciphertext); err != nil {
			log.Fatalf("publish failed: %v", err)
		}

		log.Println("Sent Round 1 Message: Caller --> Recipient")
	}
}

// func rtuRound1(oob *subscriber.Controller, callState *protocol.CallState) {

// }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Initialize call state
	callState := createCallState()

	// Derive shared key and update state
	RunKeyDerivation(ctx, callState)

	// create controller to handle pub-sub communication
	oobController, err := subscriber.NewController(callState)
	if err != nil {
		panic(err)
	}

	//to-do: add check to run akeRound1 only if no state is found
	akeRound1(oobController, callState)
	//else rtuRound1(oobController, callState)

	// Now start the subscription service
	oobController.Start(func(data []byte) {
		message, err := getMessage(callState, data)
		if err != nil {
			log.Printf("error getting message stream: %v", err)
		}

		log.Printf("New Message. Type:%s, Sender:%s, Round:%d", message.Type, message.SenderId, message.Round)

		if message.SenderId == callState.SenderId {
			log.Printf("Ignoring self-authored message")
			return
		}

		if message.IsAke() {
			var akeMsg protocol.AkeMessage
			message.DecodePayload(&akeMsg)

			if akeMsg.IsRoundOne() && callState.IamRecipient() {
				log.Println("Handling Round 1 Message: Recipient --> Caller")

				response, err := protocol.AkeRound2RecipientToCaller(callState, &akeMsg)
				if err != nil {
					log.Printf("failed responding to ake round 1: %v", err)
				}
				if err := oobController.Send(response); err != nil {
					log.Printf("failed responding to ake round 1: %v", err)
				}

				log.Printf("Computed Shared Secret: %x", callState.SharedKey)
				stop() // Signal completion
			}

			if akeMsg.IsRoundTwo() && callState.IamCaller() {
				log.Println("Handling Round 2 Message: Caller Finalize")
				if err := protocol.AkeRound2CallerFinalize(callState, &akeMsg); err != nil {
					log.Printf("failed to finalize recipients ake message: %v", err)
				}

				log.Printf("Computed Shared Secret: %x", callState.SharedKey)
				stop() // Signal completion
			}
		}
	})

	log.Printf("Topic: %s", callState.Topic)

	// Wait for completion or Ctrl-C
	<-ctx.Done()

	_ = oobController.Close()
}

func getMessage(callState *protocol.CallState, data []byte) (protocol.ProtocolMessage, error) {
	plaintext, err := encryption.SymDecrypt(callState.SharedKey, data)
	if err != nil {
		return protocol.ProtocolMessage{}, err
		//to-do: decrypt with public key encryption if symmetric fails
	}
	message := protocol.ProtocolMessage{}
	message.Unmarshal(plaintext)
	return message, nil
}