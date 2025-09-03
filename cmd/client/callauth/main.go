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

func akeInit(oob *subscriber.Controller, callState *protocol.CallState) {
	err := protocol.InitAke(callState)
	if err != nil {
		log.Fatalf("failed to init AKE: %v", err)
	}

	if callState.IsOutgoing {
		ciphertext, err := protocol.AkeInitCallerToRecipient(callState)

		if err != nil {
			log.Fatalf("failed creating AkeInit Caller --> Recipient: %v", err)
		}

		if err := oob.Send(ciphertext); err != nil {
			log.Fatalf("publish failed: %v", err)
		}

		log.Println("Sent AkeInit Message: Caller --> Recipient")
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

	//to-do: add check to run akeInit only if no state is found
	akeInit(oobController, callState)
	//else rtuRound1(oobController, callState)

	// Now start the subscription service
	oobController.Start(func(data []byte) {
		message, err := getMessage(callState, data)
		if err != nil {
			log.Printf("error getting message stream: %v", err)
		}

		log.Printf("New Message. Type:%s, Sender:%s", message.Type, message.SenderId)

		if message.SenderId == callState.SenderId {
			log.Printf("Ignoring self-authored message")
			return
		}

		// Check for bye message - applies to both roles
		if message.IsBye() {
			log.Println("Received bye message - shutting down")
			stop()
			return
		}

		// === Recipient Logic ===
		if callState.IamRecipient() {
			if message.IsAkeInit() {
				log.Println("Handling AkeInit Message: Recipient --> Caller")

				response, err := protocol.AkeResponseRecipientToCaller(callState, &message)
				if err != nil {
					log.Printf("failed responding to ake init: %v", err)
					return
				}
				if err := oobController.Send(response); err != nil {
					log.Printf("failed responding to ake init: %v", err)
					return
				}

				log.Printf("Computed Shared Secret: %x", callState.SharedKey)
			}

			if message.IsAkeComplete() {
				log.Println("Received AkeComplete message - AKE protocol finished")
				// Here we would transition to RTU protocol later
			}
		}

		// === Caller Logic ===
		if callState.IamCaller() {
			if message.IsAkeResponse() {
				log.Println("Handling AkeResponse Message: Caller Finalize")
				if err := protocol.AkeFinalizeCaller(callState, &message); err != nil {
					log.Printf("failed to finalize ake response: %v", err)
					return
				}

				log.Printf("Computed Shared Secret: %x", callState.SharedKey)

				// Send AkeComplete to signal protocol completion
				completeMsg, err := protocol.AkeCompleteSendToCaller(callState)
				if err != nil {
					log.Printf("failed to create ake complete message: %v", err)
					return
				}
				if err := oobController.Send(completeMsg); err != nil {
					log.Printf("failed to send ake complete message: %v", err)
					return
				}
				log.Println("Sent AkeComplete message to recipient")
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
