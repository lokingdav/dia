package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"

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

	// Non-interactive experiment mode
	if cfg.ExperimentMode != "" {
		if cfg.ExperimentPhone == "" {
			log.Fatalf("-phone is required when -experiment is set")
		}
		if err := runExperiment(controller, cfg.ExperimentMode, cfg.ExperimentPhone, cfg.ExperimentRuns, cfg.ExperimentConcurrency); err != nil {
			log.Fatalf("Experiment failed: %v", err)
		}
		controller.Stop()
		return
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
	log.Println("  dial <phone> [proto] - Initiate outgoing call (proto=on|off)")
	log.Println("  list               - List active calls")
	log.Println("  oda <callid> <attrs> - Trigger ODA verification")
	log.Println("  run <baseline|integrated> <phone> <runs> [concurrency] - Run experiment")
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
				fmt.Println("Usage: dial <phone_number> [proto_on|proto_off]")
				continue
			}
			phone := parts[1]
			protoEnabled := true
			if len(parts) >= 3 {
				switch strings.ToLower(parts[2]) {
				case "on", "true", "1", "proto_on":
					protoEnabled = true
				case "off", "false", "0", "proto_off":
					protoEnabled = false
				default:
					fmt.Println("proto must be on|off")
					continue
				}
			}
			attemptID, err := controller.InitiateOutgoingCall(phone, protoEnabled)
			if err != nil {
				fmt.Printf("Dial failed: %v\n", err)
			} else {
				fmt.Printf("Dial started: attempt=%s\n", attemptID)
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

		case "run":
			if len(parts) < 4 {
				fmt.Println("Usage: run <baseline|integrated> <phone> <runs> [concurrency]")
				continue
			}
			mode := parts[1]
			phone := parts[2]
			runs := 0
			fmt.Sscanf(parts[3], "%d", &runs)
			conc := 1
			if len(parts) >= 5 {
				fmt.Sscanf(parts[4], "%d", &conc)
			}
			if err := runExperiment(controller, mode, phone, runs, conc); err != nil {
				fmt.Printf("Run failed: %v\n", err)
			}

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

func runExperiment(controller *sipcontroller.Controller, mode, phone string, runs, concurrency int) error {
	if runs <= 0 {
		return fmt.Errorf("runs must be > 0")
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	protocolEnabled := false
	switch strings.ToLower(mode) {
	case "baseline":
		protocolEnabled = false
	case "integrated":
		protocolEnabled = true
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}

	log.Printf("[Experiment] mode=%s phone=%s runs=%d concurrency=%d", mode, phone, runs, concurrency)

	results := controller.Results()
	started := 0
	completed := 0
	var mu sync.Mutex
	inFlight := make(map[string]struct{})

	startOne := func() error {
		attemptID, err := controller.InitiateOutgoingCall(phone, protocolEnabled)
		if err != nil {
			return err
		}
		mu.Lock()
		inFlight[attemptID] = struct{}{}
		mu.Unlock()
		started++
		return nil
	}

	// Prime pipeline
	for started < runs && started < concurrency {
		if err := startOne(); err != nil {
			return err
		}
	}

	for completed < runs {
		res := <-results
		mu.Lock()
		_, ok := inFlight[res.AttemptID]
		if ok {
			delete(inFlight, res.AttemptID)
		}
		mu.Unlock()
		if !ok {
			continue
		}

		if data, err := json.Marshal(res); err == nil {
			fmt.Println(string(data))
		}
		completed++

		if started < runs {
			if err := startOne(); err != nil {
				return err
			}
		}
	}

	return nil
}
