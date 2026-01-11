package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dense-identity/denseid/internal/subscriber"
	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

type AppConfig struct {
	RelayServerAddr string
	UseTLS          bool
	ODADelaySec     int
	ODAAttrs        []string
	Bye             bool
}

type RuntimeState struct {
	odaTriggered atomic.Bool
	odaHandled   atomic.Bool
	ruaComplete  atomic.Bool
	byeSent      atomic.Bool
}

func parseFlags() (envFile, phone string, outgoing bool, appCfg *AppConfig) {
	dial := flag.String("dial", "", "The phone number to dial (outgoing call)")
	receive := flag.String("receive", "", "The phone number to receive call from (incoming call)")
	envfile := flag.String("env", "", ".env file containing DIA subscriber credentials")
	relayAddr := flag.String("relay", "localhost:50052", "Relay server address")
	useTLS := flag.Bool("tls", false, "Use TLS for relay connection")
	odaDelay := flag.Int("oda", -1, "Seconds to wait after RUA before initiating ODA (>=0 enables; default -1 disables)")
	odaAttrs := flag.String("oda-attrs", "name,issuer", "Comma-separated list of ODA attribute names to request (used with --oda)")
	bye := flag.Bool("bye", false, "Send DIA BYE and exit when done")
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
		ODADelaySec:     *odaDelay,
		ODAAttrs:        parseCommaList(*odaAttrs),
		Bye:             *bye,
	}

	return *envfile, phone, outgoing, appCfg
}

func parseCommaList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

	runtime := &RuntimeState{}

	// Start the tunnel and handle incoming messages
	controller.Start(func(data []byte) {
		handleMessage(callState, controller, appCfg, runtime, data, stop)
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

func handleMessage(callState *dia.CallState, controller *subscriber.Controller, cfg *AppConfig, runtime *RuntimeState, data []byte, stop context.CancelFunc) {
	msg, err := dia.ParseMessage(data)
	if err != nil {
		log.Printf("Failed to parse message (%d bytes): %v - data[0:min(20,len)]: %x", len(data), err, data[:min(20, len(data))])
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

	// Handle bye message (always honor, even if topic mismatches due to in-flight switching)
	if msg.Type() == dia.MsgBye {
		log.Println("Received BYE message - shutting down")
		stop()
		return
	}

	// Filter by current active topic
	if msgTopic != currentTopic {
		log.Printf("Ignoring message from inactive topic: %s (current: %s)", msgTopic, currentTopic)
		return
	}

	// Handle heartbeat message
	if msg.Type() == dia.MsgHeartbeat {
		log.Println("Received HEARTBEAT message - channel active")
		return
	}

	// Handle ODA messages (bidirectional, same logic for both parties)
	if msg.Type() == dia.MsgODARequest {
		log.Println("Handling ODA Request")
		response, err := callState.ODAResponse(data)
		if err != nil {
			log.Printf("Failed to create ODA response: %v", err)
			return
		}
		if err := controller.Send(response); err != nil {
			log.Printf("Failed to send ODA response: %v", err)
			return
		}
		log.Println("Sent ODA Response")
		runtime.odaHandled.Store(true)
		// Do not exit here. The ODA initiator will terminate the session by sending BYE.
		return
	}

	if msg.Type() == dia.MsgODAResponse {
		log.Println("Handling ODA Response")
		verification, err := callState.ODAVerify(data)
		if err != nil {
			log.Printf("Failed to verify ODA response: %v", err)
			return
		}
		log.Printf(
			"ODA Verification: verified=%v issuer=%s credential_type=%s disclosed=%v",
			verification.Verified,
			verification.Issuer,
			verification.CredentialType,
			verification.DisclosedAttributes,
		)
		runtime.odaHandled.Store(true)
		// If we triggered ODA, we're done after verifying the response.
		if runtime.odaTriggered.Load() && cfg != nil && cfg.Bye {
			sendByeThenStop(runtime, callState, controller, stop)
		}
		return
	}

	// Route based on role
	if callState.IsRecipient() {
		handleRecipientMessage(callState, controller, cfg, runtime, msg, data, stop)
	} else if callState.IsCaller() {
		handleCallerMessage(callState, controller, cfg, runtime, msg, data, stop)
	}
}

// handleRecipientMessage handles messages for the recipient (Bob)
func handleRecipientMessage(callState *dia.CallState, controller *subscriber.Controller, cfg *AppConfig, runtime *RuntimeState, msg *dia.Message, rawData []byte, stop context.CancelFunc) {
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

		ticket, err := callState.Ticket()
		if err != nil {
			log.Printf("Failed to get ticket: %v", err)
			return
		}

		// Subscribe to RUA topic (with replay)
		if err := controller.SubscribeToNewTopicWithPayload(ruaTopic, nil, ticket); err != nil {
			log.Printf("Failed to subscribe to RUA topic: %v", err)
			return
		}
		log.Printf("Subscribed to RUA topic: %s", ruaTopic)

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
			logRemoteParty(remoteParty)
		}

		maybeScheduleODA(cfg, runtime, callState, controller)
		runtime.ruaComplete.Store(true)

		// Session termination is BYE-driven.
		// In the common dev setup, only the recipient ends the session after RUA
		// when no ODA is configured.
		if cfg != nil && cfg.Bye && cfg.ODADelaySec < 0 {
			sendByeThenStop(runtime, callState, controller, stop)
		}
	}
}

// handleCallerMessage handles messages for the caller (Alice)
func handleCallerMessage(callState *dia.CallState, controller *subscriber.Controller, cfg *AppConfig, runtime *RuntimeState, msg *dia.Message, rawData []byte, stop context.CancelFunc) {
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

		// Send AKE complete ASAP on the AKE topic so the recipient can finalize.
		// (Matches sipcontroller ordering: don't put topic switching in front of the unblock message.)
		if err := controller.SendToTopic(oldTopic, complete, nil); err != nil {
			log.Printf("Failed to send AKE complete: %v", err)
			return
		}
		log.Println("Sent AKE Complete")

		// Derive RUA topic
		ruaTopic, err := callState.RUADeriveTopic()
		if err != nil {
			log.Printf("Failed to derive RUA topic: %v", err)
			return
		}

		// Create RUA request
		ruaRequest, err := callState.RUARequest()
		if err != nil {
			log.Printf("Failed to create RUA request: %v", err)
			return
		}

		// Get ticket for topic creation
		ticket, _ := callState.Ticket()

		// Transition state to RUA
		if err := callState.TransitionToRUA(); err != nil {
			log.Printf("Failed to transition to RUA: %v", err)
			return
		}

		// Subscribe to RUA topic with RUA request as payload
		if err := controller.SubscribeToNewTopicWithPayload(ruaTopic, ruaRequest, ticket); err != nil {
			log.Printf("Failed to subscribe to RUA topic: %v", err)
			return
		}
		log.Printf("Subscribed to RUA topic: %s", ruaTopic)

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
			logRemoteParty(remoteParty)
		}

		maybeScheduleODA(cfg, runtime, callState, controller)
		runtime.ruaComplete.Store(true)
		// Caller waits for recipient BYE.
	}
}

func maybeScheduleODA(cfg *AppConfig, runtime *RuntimeState, callState *dia.CallState, controller *subscriber.Controller) {
	if cfg == nil || runtime == nil || callState == nil || controller == nil {
		return
	}
	if cfg.ODADelaySec < 0 || len(cfg.ODAAttrs) == 0 {
		return
	}
	if runtime.odaTriggered.Swap(true) {
		return
	}

	delay := time.Duration(cfg.ODADelaySec) * time.Second
	log.Printf("Scheduling ODA after RUA: delay=%s attrs=%v", delay, cfg.ODAAttrs)

	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		request, err := callState.ODARequest(cfg.ODAAttrs)
		if err != nil {
			log.Printf("Failed to create ODA request: %v", err)
			return
		}
		if err := controller.Send(request); err != nil {
			log.Printf("Failed to send ODA request: %v", err)
			return
		}
		log.Println("Sent ODA Request")
	}()
}

func sendByeThenStop(runtime *RuntimeState, callState *dia.CallState, controller *subscriber.Controller, stop context.CancelFunc) {
	if stop == nil {
		return
	}
	if runtime == nil || callState == nil || controller == nil {
		stop()
		return
	}
	if runtime.byeSent.Swap(true) {
		stop()
		return
	}

	bye, err := callState.CreateByeMessage()
	if err != nil {
		log.Printf("Failed to create BYE message: %v", err)
		stop()
		return
	}
	// Send BYE and exit immediately. We don't wait for a re-echo.
	if err := controller.SendImmediate(bye); err != nil {
		log.Printf("Failed to send BYE message: %v", err)
		stop()
		return
	}
	log.Println("Sent BYE")
	stop()
}

func logRemoteParty(remoteParty *dia.RemoteParty) {
	if remoteParty == nil {
		return
	}
	log.Printf(
		"\nRemote party:\n\tname=%s\n\tphone=%s\n\tverified=%v\n\tlogo=%s",
		remoteParty.Name,
		remoteParty.Phone,
		remoteParty.Verified,
		remoteParty.Logo,
	)
}
