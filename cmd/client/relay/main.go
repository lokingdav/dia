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
	"github.com/dense-identity/denseid/internal/subscriber"
	"github.com/google/uuid"
)

func main() {
	number := flag.String("phone", "", "Phone number to dial/recieve calls to/from")
	action := flag.String("action", "dial", "Either dial/receive")
	flag.Parse()

	senderId := uuid.NewString()
	ticket := []byte("opaque-ticket")
	topic := "hello!world"

	cfg, err := config.New[subscriber.Config]()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctrl, err := subscriber.NewController(
		cfg.RsHost, cfg.RsPort, cfg.UseTls, 
		topic,
		ticket, 
		senderId,
	)
	if err != nil { panic(err) }

	// Start receiving (non-blocking)
	ctrl.Start(func(b []byte) {
		fmt.Println("RX:", string(b))
	})

	// Wait for Ctrl-C, then close gracefully
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	_ = ctrl.Close()
}
