package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/subscriber"
	"github.com/google/uuid"
)

type calldetail struct {
	CallerId string
	Recipient string
	Ts string
}

func newCalldetail(phone, action, myPhone string) *calldetail {
	var callerId, recipient string
	if action == "dial" {
		callerId = myPhone
		recipient = phone
	} else {
		callerId = phone
		recipient = myPhone
	}
	return &calldetail{
		CallerId: callerId,
		Recipient: recipient,
		Ts: datetime.GetTimestamp(),
	}
}

func main() {
	phone := flag.String("phone", "", "Phone number to dial/recieve calls to/from")
	action := flag.String("action", "dial", "Either dial/receive")
	flag.Parse()

	if *phone == "" {
		log.Fatal("--phone is required")
	}
	if *action == "" {
		log.Fatal("--action is required to be either dial or receive")
	}

	senderId := uuid.NewString()
	topic := "hello!world"

	cfg, err := config.New[subscriber.Config]()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	callDetail := newCalldetail(*phone, *action, cfg.MyPhone)

	secretKey, err := subscriber.RunKeyDerivation(callDetail.CallerId, callDetail.Ts, cfg.SampleTicket, cfg.KsAddr)

	if err != nil {
		log.Fatalf("Error deriving key: %w", err)
	}

	log.Printf("Secret Key: %x", secretKey)

	ctrl, err := subscriber.NewController(
		cfg.RsHost, cfg.RsPort, cfg.UseTls, 
		topic,
		cfg.SampleTicket, 
		senderId,
	)
	if err != nil { panic(err) }

	// Start receiving (non-blocking)
	ctrl.Start(func(b []byte) {
		fmt.Println("RX:", string(b))
	})

	if *action == "dial" { // send initial auth info
		if err := ctrl.Send([]byte("AUTH_INIT|from=+15551234567")); err != nil {
			println("publish failed:", err.Error())
		}
	}

	// Wait for Ctrl-C, then close gracefully
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	_ = ctrl.Close()
}
