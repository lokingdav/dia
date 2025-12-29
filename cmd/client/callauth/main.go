package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/dense-identity/denseid/internal/subscriber"
	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

type AppConfig struct {
	RelayServerAddr string
	UseTLS          bool
}

func parseFlags() (envFile, phone string, outgoing bool, appCfg *AppConfig) {
	dial := flag.String("dial", "", "The phone number to dial (outgoing call)")
	receive := flag.String("receive", "", "The phone number to receive call from (incoming call)")
	envfile := flag.String("env", "", ".env file containing DIA subscriber credentials")
	relayAddr := flag.String("relay", "localhost:50052", "Relay server address")
	useTLS := flag.Bool("tls", false, "Use TLS for relay connection")
	flag.Parse()

	if *dial == "" && *receive == "" {
		log.Fatal("--dial or --receive option is required")
	}
	if *dial != "" && *receive != "" {
		log.Fatal("You cannot specify both --dial and --receive")
	}
	if *envfile == "" {
		log.Fatal("--env is required for subscriber authentication")
	}

	if *dial == "" {
		outgoing = false
		phone = *receive
	} else {
		outgoing = true
		phone = *dial
	}

	appCfg = &AppConfig{
		RelayServerAddr: *relayAddr,
		UseTLS:          *useTLS,
	}

	return *envfile, phone, outgoing, appCfg
}

func loadDIAConfig(envFile string) (*dia.Config, error) {
	content, err := os.ReadFile(envFile)
	if err != nil {
		return nil, fmt.Errorf("reading env file: %w", err)
	}
	return dia.ConfigFromEnv(string(content))
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	envFile, phone, outgoing, appCfg := parseFlags()

	// Load DIA config from env file
	diaCfg, err := loadDIAConfig(envFile)
	if err != nil {
		log.Fatalf("Failed to load DIA config: %v", err)
	}

	// Create call state
	callState, err := dia.NewCallState(diaCfg, phone, outgoing)
	if err != nil {
		log.Fatalf("Failed to create call state: %v", err)
	}

	// Initialize AKE (generates DH keys, computes topic)
	if err := callState.AKEInit(); err != nil {
		log.Fatalf("Failed to init AKE: %v", err)
	}

	akeTopic, _ := callState.AKETopic()
	senderID, _ := callState.SenderID()

	fmt.Println("===== Call Details =====")
	fmt.Printf("--> Outgoing: %v\n", outgoing)
	fmt.Printf("--> Other Party: %s\n", phone)
	fmt.Printf("--> Sender ID: %s\n", senderID)
	fmt.Printf("--> AKE Topic: %s\n\n", akeTopic)

	// Create controller (will subscribe to AKE topic)
	controller, err := subscriber.NewController(callState, &subscriber.ControllerConfig{
		RelayServerAddr: appCfg.RelayServerAddr,
		UseTLS:          appCfg.UseTLS,
	})
	if err != nil {
		log.Fatalf("Failed to create controller: %v", err)
	}

	// Start the tunnel and handle incoming messages
	controller.Start(func(data []byte) {
		handleMessage(callState, controller, data, stop)
	})

	// For outgoing calls, send AKE request
	if outgoing {
		request, err := callState.AKERequest()
		if err != nil {
			log.Fatalf("Failed to create AKE request: %v", err)
		}
		if err := controller.Send(request); err != nil {
			log.Fatalf("Failed to send AKE request: %v", err)
		}
		log.Println("Sent AKE Request")
	}

	currentTopic, _ := callState.CurrentTopic()
	log.Printf("Listening on topic: %s", currentTopic)

	<-ctx.Done()
	_ = controller.Close()
	log.Println("Shutting down...")
}

func handleMessage(callState *dia.CallState, controller *subscriber.Controller, data []byte, stop context.CancelFunc) {
	msg, err := dia.ParseMessage(data)
	if err != nil {
		log.Printf("Failed to parse message: %v", err)
		return
	}

	msgSenderID, _ := msg.SenderID()
	msgTopic, _ := msg.Topic()
	mySenderID, _ := callState.SenderID()
	currentTopic, _ := callState.CurrentTopic()

	log.Printf("Received message type=%d from=%s topic=%s", msg.Type(), msgSenderID, msgTopic)

	// Ignore self-authored messages
	if msgSenderID == mySenderID {
		log.Printf("Ignoring self-authored message")
		return
	}

	// Filter by current active topic
	if msgTopic != currentTopic {
		log.Printf("Ignoring message from inactive topic: %s (current: %s)", msgTopic, currentTopic)
		return
	}

	// Handle bye message
	if msg.Type() == dia.MsgBye {
		log.Println("Received BYE message - shutting down")
		stop()
		return
	}

	// Route based on role
	if callState.IsRecipient() {
		handleRecipientMessage(callState, controller, msg, data)
	} else if callState.IsCaller() {
		handleCallerMessage(callState, controller, msg, data)
	}
}

// handleRecipientMessage handles messages for the recipient (Bob)
func handleRecipientMessage(callState *dia.CallState, controller *subscriber.Controller, msg *dia.Message, rawData []byte) {
	switch msg.Type() {
	case dia.MsgAKERequest:
		log.Println("Handling AKE Request")
		response, err := callState.AKEResponse(rawData)
		if err != nil {
			log.Printf("Failed to create AKE response: %v", err)
			return
		}
		if err := controller.Send(response); err != nil {
			log.Printf("Failed to send AKE response: %v", err)
			return
		}
		log.Println("Sent AKE Response")

	case dia.MsgAKEComplete:
		log.Println("Handling AKE Complete")
		if err := callState.AKEFinalize(rawData); err != nil {
			log.Printf("Failed to finalize AKE: %v", err)
			return
		}

		sharedKey, _ := callState.SharedKey()
		log.Printf("AKE Complete! Shared key: %x", sharedKey)

		// Derive RUA topic and transition
		ruaTopic, err := callState.RUADeriveTopic()
		if err != nil {
			log.Printf("Failed to derive RUA topic: %v", err)
			return
		}

		// Transition state to RUA first
		if err := callState.TransitionToRUA(); err != nil {
			log.Printf("Failed to transition to RUA: %v", err)
			return
		}

		// Swap to RUA topic (with replay)
		if err := controller.SwapToTopic(ruaTopic, nil, nil); err != nil {
			log.Printf("Failed to swap to RUA topic: %v", err)
			return
		}
		log.Printf("Swapped to RUA topic: %s", ruaTopic)

		// Initialize RUA
		if err := callState.RUAInit(); err != nil {
			log.Printf("Failed to init RUA: %v", err)
			return
		}
		log.Println("RUA initialized, waiting for RUA request...")

	case dia.MsgRUARequest:
		log.Println("Handling RUA Request")
		response, err := callState.RUAResponse(rawData)
		if err != nil {
			log.Printf("Failed to create RUA response: %v", err)
			return
		}
		if err := controller.Send(response); err != nil {
			log.Printf("Failed to send RUA response: %v", err)
			return
		}

		sharedKey, _ := callState.SharedKey()
		log.Printf("RUA Complete! New shared key: %x", sharedKey)

		remoteParty, err := callState.RemoteParty()
		if err == nil && remoteParty != nil {
			log.Printf("Remote party: %s (%s) logo=%s verified=%v", remoteParty.Name, remoteParty.Phone, remoteParty.Logo, remoteParty.Verified)
		}
	}
}

// handleCallerMessage handles messages for the caller (Alice)
func handleCallerMessage(callState *dia.CallState, controller *subscriber.Controller, msg *dia.Message, rawData []byte) {
	switch msg.Type() {
	case dia.MsgAKEResponse:
		log.Println("Handling AKE Response")

		// Get old topic before processing
		oldTopic, _ := callState.CurrentTopic()

		// Process response and get complete message
		complete, err := callState.AKEComplete(rawData)
		if err != nil {
			log.Printf("Failed to complete AKE: %v", err)
			return
		}

		sharedKey, _ := callState.SharedKey()
		log.Printf("AKE Complete! Shared key: %x", sharedKey)

		// Derive RUA topic
		ruaTopic, err := callState.RUADeriveTopic()
		if err != nil {
			log.Printf("Failed to derive RUA topic: %v", err)
			return
		}

		// Create RUA request before transitioning
		ruaRequest, err := callState.RUARequest()
		if err != nil {
			log.Printf("Failed to create RUA request: %v", err)
			return
		}

		// Transition state to RUA
		if err := callState.TransitionToRUA(); err != nil {
			log.Printf("Failed to transition to RUA: %v", err)
			return
		}

		// Get ticket for topic creation
		ticket, _ := callState.Ticket()

		// Subscribe to RUA topic with RUA request as payload
		if err := controller.SubscribeToNewTopicWithPayload(ruaTopic, ruaRequest, ticket); err != nil {
			log.Printf("Failed to subscribe to RUA topic: %v", err)
			return
		}
		log.Printf("Subscribed to RUA topic: %s", ruaTopic)

		// Send AKE complete on old topic
		if err := controller.SendToTopic(oldTopic, complete, nil); err != nil {
			log.Printf("Failed to send AKE complete: %v", err)
			return
		}
		log.Println("Sent AKE Complete")

	case dia.MsgRUAResponse:
		log.Println("Handling RUA Response")
		if err := callState.RUAFinalize(rawData); err != nil {
			log.Printf("Failed to finalize RUA: %v", err)
			return
		}

		sharedKey, _ := callState.SharedKey()
		log.Printf("RUA Complete! New shared key: %x", sharedKey)

		remoteParty, err := callState.RemoteParty()
		if err == nil && remoteParty != nil {
			log.Printf("Remote party: %s (%s) verified=%v logo=%s", 
				remoteParty.Name, remoteParty.Phone, remoteParty.Verified, remoteParty.Logo,
			)
		}
	}
}
