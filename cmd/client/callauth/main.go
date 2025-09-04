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

	cfg, err := config.New[config.SubscriberConfig]()
	if err != nil {
		log.Fatalf("failed to load subscriber config: %v", err)
	}
	if err := cfg.ParseKeysAsBytes(); err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}

	state := protocol.NewCallState(cfg, phoneNumber, outgoing)

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
	if err := kdClient.Close(); err != nil {
		log.Fatalf("unable to close keyderivation client: %v", err)
	}
	callState.SetSharedKey(sharedKey)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// 1) Call state & key derivation
	callState := createCallState()
	RunKeyDerivation(ctx, callState)

	// 2) IMPORTANT: Initialize AKE BEFORE creating the Relay controller
	if err := protocol.InitAke(callState); err != nil {
		log.Fatalf("failed to init AKE: %v", err)
	}
	log.Printf("AKE Topic: %s", callState.AkeTopic)

	// 3) Now create the controller (Session will subscribe to CurrentTopic = AkeTopic)
	oobController, err := subscriber.NewController(callState)
	if err != nil {
		panic(err)
	}

	// 4) Start the Tunnel (initial SUBSCRIBE will use AkeTopic)
	oobController.Start(func(data []byte) {
		message, err := getMessage(callState, data)
		if err != nil {
			log.Printf("error getting message stream: %v", err)
			return
		}

		log.Printf("New Message. Type:%s, Sender:%s, Topic:%s", message.Type, message.SenderId, message.Topic)

		// (Server already suppresses self-echo; this is extra safety)
		if message.SenderId == callState.SenderId {
			log.Printf("Ignoring self-authored message")
			return
		}

		// Filter by current active topic
		if message.Topic != callState.GetCurrentTopic() {
			log.Printf("Ignoring message from inactive topic: %s (current: %s)", message.Topic, callState.GetCurrentTopic())
			return
		}

		if message.IsBye() {
			log.Println("Received bye message - shutting down")
			stop()
			return
		}

		// === Recipient (Bob) ===
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

				// Derive RTU topic
				rtuTopic := protocol.DeriveRtuTopic(callState.SharedKey)

				// FIX: move our state FIRST so replay from rtuTopic isn't filtered
				callState.TransitionToRtu(rtuTopic)

				// Ask server to swap (replay then live)
				if err := oobController.SwapToTopic(rtuTopic, nil, nil); err != nil {
					log.Printf("failed to swap to RTU topic: %v", err)
					// (Optional) revert: callState.TransitionToRtu(callState.AkeTopic)
					return
				}

				log.Printf("Swapped to RTU topic: %s", rtuTopic)
			}

			if message.IsRtuInit() {
				log.Println("Received RtuInit message - RTU protocol started")
				// Handle RTU init...
			}
		}

		// === Caller (Alice) ===
		if callState.IamCaller() {
			if message.IsAkeResponse() {
				log.Println("Handling AkeResponse Message: Caller Finalize")
				if err := protocol.AkeFinalizeCaller(callState, &message); err != nil {
					log.Printf("failed to finalize ake response: %v", err)
					return
				}
				log.Printf("Computed Shared Secret: %x", callState.SharedKey)

				// Capture old (AKE) topic BEFORE switching
				oldTopic := callState.AkeTopic

				// Create RTU init (transitions state to RTU inside)
				rtuTopic, rtuInitMsg, err := protocol.CreateRtuInitForCaller(callState)
				if err != nil {
					log.Printf("failed to create RTU init: %v", err)
					return
				}

				// Subscribe to RTU and piggy-back RTU init
				if err := oobController.SubscribeToNewTopicWithPayload(rtuTopic, rtuInitMsg, callState.Ticket); err != nil {
					log.Printf("failed to subscribe+init on RTU topic: %v", err)
					return
				}
				log.Printf("Subscribed to RTU topic (with init): %s", rtuTopic)

				// Notify Bob on the OLD AKE topic to move
				completeMsg, err := protocol.AkeCompleteSendToCaller(callState)
				if err != nil {
					log.Printf("failed to create ake complete message: %v", err)
					return
				}
				if err := oobController.SendToTopic(oldTopic, completeMsg, nil); err != nil {
					log.Printf("failed to send AkeComplete on old topic: %v", err)
					return
				}
				log.Println("Sent AkeComplete on old topic")
			}
		}
	})

	// 5) For outgoing calls, after Tunnel is started, send AkeInit
	if callState.IsOutgoing {
		ciphertext, err := protocol.AkeInitCallerToRecipient(callState)
		if err != nil {
			log.Fatalf("failed creating AkeInit Caller --> Recipient: %v", err)
		}
		if err := oobController.Send(ciphertext); err != nil {
			log.Fatalf("publish failed: %v", err)
		}
		log.Println("Sent AkeInit Message: Caller --> Recipient")
	}

	log.Printf("Active Topic: %s", callState.GetCurrentTopic())

	<-ctx.Done()
	_ = oobController.Close()
}

func getMessage(callState *protocol.CallState, data []byte) (protocol.ProtocolMessage, error) {
	plaintext, err := encryption.SymDecrypt(callState.SharedKey, data)
	if err != nil {
		return protocol.ProtocolMessage{}, err
		// TODO: fallback to public key encryption if symmetric fails
	}
	var message protocol.ProtocolMessage
	_ = message.Unmarshal(plaintext)
	return message, nil
}
