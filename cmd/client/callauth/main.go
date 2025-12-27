package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/helpers"
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
	fmt.Printf("--> Src: %s\n", state.Src)
	fmt.Printf("--> Dst: %s\n\n", state.Dst)

	return &state
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// 1) Call state & key derivation
	callState := createCallState()
	// RunKeyDerivation(ctx, callState)

	// 2) IMPORTANT: Initialize AKE BEFORE creating the Relay controller
	if err := protocol.InitAke(callState); err != nil {
		log.Fatalf("failed to init AKE: %v", err)
	}
	log.Printf("AKE Topic:\n\t%s", callState.GetAkeTopic())

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

		if protocol.IsBye(message) {
			log.Println("Received bye message - shutting down")
			stop()
			return
		}

		// === Recipient (Bob) ===
		if callState.IamRecipient() {
			if protocol.IsAkeRequest(message) {
				log.Println("Handling AkeRequest Message: Recipient --> Caller")

				response, err := protocol.AkeResponse(callState, message)
				if err != nil {
					log.Printf("failed responding to ake init: %v", err)
					return
				}
				if err := oobController.Send(response); err != nil {
					log.Printf("failed responding to ake init: %v", err)
					return
				}
			}

			if protocol.IsAkeComplete(message) {
				log.Println("Received AkeComplete message - AKE protocol finished")
				err := protocol.AkeFinalize(callState, message)

				if err != nil {
					log.Printf("failed to finalize ake: %v", err)
				}

				log.Printf("Computed Shared Secret: %x", callState.SharedKey)

				// Derive RUA topic
				ruaTopic := protocol.DeriveRuaTopic(callState)
				// FIX: move our state FIRST so replay from ruaTopic isn't filtered
				callState.TransitionToRua(ruaTopic)

				// Ask server to swap (replay then live)
				if err := oobController.SwapToTopic(helpers.EncodeToHex(ruaTopic), nil, nil); err != nil {
					log.Printf("failed to swap to RUA topic: %v", err)
					// (Optional) revert: callState.TransitionToRua(callState.AkeTopic)
					return
				}

				log.Printf("Swapped to RUA topic: %s", ruaTopic)

				// Initialize RTU for recipient
				if err := protocol.InitRTU(callState); err != nil {
					log.Printf("failed to init RTU: %v", err)
					return
				}
				log.Println("RTU initialized, waiting for RuaRequest...")
			}

			if protocol.IsRuaRequest(message) {
				log.Println("Received RuaRequest message - sending RuaResponse")

				response, err := protocol.RuaResponse(callState, message)
				if err != nil {
					log.Printf("failed to create RuaResponse: %v", err)
					return
				}

				if err := oobController.Send(response); err != nil {
					log.Printf("failed to send RuaResponse: %v", err)
					return
				}

				log.Printf("RUA completed! New shared secret: %x", callState.SharedKey)
			}
		}

		// === Caller (Alice) ===
		if callState.IamCaller() {
			if protocol.IsAkeResponse(message) {
				log.Println("Handling AkeResponse Message: Caller Finalize")
				complete, err := protocol.AkeComplete(callState, message)
				if err != nil {
					log.Printf("failed to process AkeResponse: %v", err)
					return
				}

				log.Printf("Computed Shared Secret: %x", callState.SharedKey)

				// Capture old (AKE) topic BEFORE switching
				oldTopic := callState.Ake.Topic

				ruaReq, err := protocol.RuaRequest(callState)

				// Create RUA init (transitions state to RUA inside)
				callState.TransitionToRua(callState.Rua.Topic)

				if err != nil {
					log.Printf("failed to create RUA init: %v", err)
					return
				}

				ruaTpcStr := helpers.EncodeToHex(callState.Rua.Topic)

				// Subscribe to RUA and piggy-back RUA init
				if err := oobController.SubscribeToNewTopicWithPayload(ruaTpcStr, ruaReq, callState.Ticket); err != nil {
					log.Printf("failed to subscribe+init on RUA topic: %v", err)
					return
				}
				log.Printf("Subscribed to RUA topic (with init): %s", ruaTpcStr)

				// Send response to Bob
				if err := oobController.SendToTopic(helpers.EncodeToHex(oldTopic), complete, nil); err != nil {
					log.Printf("failed to send AkeComplete on old topic: %v", err)
					return
				}
				log.Println("Sent AkeComplete on old topic")
			}

			if protocol.IsRuaResponse(message) {
				log.Println("Received RuaResponse message - finalizing RUA")

				err := protocol.RuaFinalize(callState, message)
				if err != nil {
					log.Printf("failed to finalize RUA: %v", err)
					return
				}

				log.Printf("RUA completed! New shared secret: %x", callState.SharedKey)
			}
		}
	})

	// 5) For outgoing calls, after Tunnel is started, send AkeRequest
	if callState.IsOutgoing {
		ciphertext, err := protocol.AkeRequest(callState)
		if err != nil {
			log.Fatalf("failed creating AkeRequest Caller --> Recipient: %v", err)
		}
		if err := oobController.Send(ciphertext); err != nil {
			log.Fatalf("publish failed: %v", err)
		}
		log.Println("Sent AkeRequest Message: Caller --> Recipient")
	}

	log.Printf("Active Topic:\n\t%s", callState.GetCurrentTopic())

	<-ctx.Done()
	_ = oobController.Close()
}

func getMessage(callState *protocol.CallState, data []byte) (*protocol.ProtocolMessage, error) {
	// ProtocolMessage envelope is always in plaintext
	// The payload inside is encrypted (PKE for AKE, symmetric for RUA)
	// Decryption is handled by DecodeAkePayload/DecodeRuaPayload in the protocol functions
	return protocol.UnmarshalMessage(data)
}
