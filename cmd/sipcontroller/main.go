package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/dense-identity/denseid/internal/sipcontroller"
)

func main() {
	// Parse configuration
	cfg := sipcontroller.ParseFlags()

	// Load DIA config
	diaCfg, err := sipcontroller.LoadDIAConfig(cfg.DIAEnvFile)
	if err != nil {
		log.Fatalf("Failed to load DIA config: %v", err)
	}

	// Create controller
	controller := sipcontroller.NewController(cfg, diaCfg)

	// Setup signal handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Start controller
	if err := controller.Start(); err != nil {
		log.Fatalf("Failed to start controller: %v", err)
	}

	log.Println("===== SIP Controller Started =====")
	log.Printf("  Baresip: %s", cfg.BaresipAddr)
	log.Printf("  Relay:   %s (TLS: %v)", cfg.RelayAddr, cfg.RelayTLS)
	log.Printf("  Timeout: %ds", cfg.TimeoutSec)
	log.Printf("  AutoODA: %v", cfg.AutoODA)
	if cfg.AutoODA {
		log.Printf("  ODA Attrs: %v", cfg.ODAAttributes)
	}
	log.Println("==================================")
	log.Println("")
	log.Println("Commands:")
	log.Println("  dial <phone>       - Initiate outgoing call")
	log.Println("  list               - List active calls")
	log.Println("  oda <callid> <attrs> - Trigger ODA verification")
	log.Println("  quit               - Exit")
	log.Println("")

	// Start command input loop
	go commandLoop(controller, stop)

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	controller.Stop()
	log.Println("Controller stopped")
}

// commandLoop reads commands from stdin
func commandLoop(controller *sipcontroller.Controller, stop context.CancelFunc) {
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := parts[0]

		switch cmd {
		case "dial":
			if len(parts) < 2 {
				fmt.Println("Usage: dial <phone_number>")
				continue
			}
			phone := parts[1]
			if err := controller.InitiateOutgoingCall(phone); err != nil {
				fmt.Printf("Dial failed: %v\n", err)
			}

		case "list":
			calls := controller.ListActiveCalls()
			if len(calls) == 0 {
				fmt.Println("No active calls")
			} else {
				fmt.Printf("Active calls (%d):\n", len(calls))
				for _, c := range calls {
					fmt.Printf("  - %s: %s (%s) state=%s dia=%v sip=%v",
						c["call_id"], c["peer_phone"], c["direction"],
						c["state"], c["dia_done"], c["sip_estab"])
					if name, ok := c["remote_name"]; ok {
						fmt.Printf(" name=%s verified=%v", name, c["verified"])
					}
					fmt.Println()
				}
			}

		case "oda":
			if len(parts) < 3 {
				fmt.Println("Usage: oda <call_id> <attr1,attr2,...>")
				continue
			}
			callID := parts[1]
			attrs := strings.Split(parts[2], ",")
			if err := controller.TriggerODAForCall(callID, attrs); err != nil {
				fmt.Printf("ODA failed: %v\n", err)
			}

		case "quit", "exit":
			stop()
			return

		case "help":
			fmt.Println("Commands:")
			fmt.Println("  dial <phone>         - Initiate outgoing call")
			fmt.Println("  list                 - List active calls")
			fmt.Println("  oda <callid> <attrs> - Trigger ODA verification")
			fmt.Println("  quit                 - Exit")

		default:
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", cmd)
		}
	}
}
