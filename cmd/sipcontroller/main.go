package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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
		if err := runExperiment(ctx, controller, cfg, cfg.ExperimentMode, cfg.ExperimentPhone, cfg.ExperimentRuns, cfg.ExperimentConcurrency); err != nil {
			if err == context.Canceled {
				// Graceful Ctrl+C
				controller.Stop()
				return
			}
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
	go commandLoop(ctx, controller, cfg, stop)

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	controller.Stop()
	log.Println("Controller stopped")
}

// commandLoop reads commands from stdin
func commandLoop(ctx context.Context, controller *sipcontroller.Controller, cfg *sipcontroller.Config, stop context.CancelFunc) {
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
			if err := runExperiment(ctx, controller, cfg, mode, phone, runs, conc); err != nil {
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

func runExperiment(ctx context.Context, controller *sipcontroller.Controller, cfg *sipcontroller.Config, mode, phone string, runs, concurrency int) error {
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

	var csvFile *os.File
	var csvWriter *csv.Writer
	if cfg != nil && strings.TrimSpace(cfg.OutputCSV) != "" {
		path := strings.TrimSpace(cfg.OutputCSV)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
			return fmt.Errorf("creating csv output dir: %w", err)
		}
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("creating csv file: %w", err)
		}
		csvFile = f
		csvWriter = csv.NewWriter(f)
		header := []string{
			"attempt_id",
			"call_id",
			"peer_phone",
			"peer_uri",
			"direction",
			"protocol_enabled",
			"dial_sent_unix_ms",
			"answered_unix_ms",
			"latency_ms",
			"oda_requested_unix_ms",
			"oda_completed_unix_ms",
			"oda_latency_ms",
			"outcome",
			"error",
		}
		if err := csvWriter.Write(header); err != nil {
			_ = f.Close()
			return fmt.Errorf("writing csv header: %w", err)
		}
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			_ = f.Close()
			return fmt.Errorf("flushing csv header: %w", err)
		}
		log.Printf("[Experiment] Writing CSV results to %s", path)
	}
	if csvFile != nil {
		defer func() {
			csvWriter.Flush()
			_ = csvFile.Close()
		}()
	}

	results := controller.Results()
	started := 0
	completed := 0
	var mu sync.Mutex
	type attemptState struct {
		printed bool
		closed  bool
	}
	inFlight := make(map[string]*attemptState)

	startOne := func() error {
		attemptID, err := controller.InitiateOutgoingCall(phone, protocolEnabled)
		if err != nil {
			return err
		}
		mu.Lock()
		inFlight[attemptID] = &attemptState{}
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
		var res sipcontroller.CallResult
		select {
		case <-ctx.Done():
			return ctx.Err()
		case res = <-results:
		}
		mu.Lock()
		st, ok := inFlight[res.AttemptID]
		mu.Unlock()
		if !ok {
			continue
		}

		// Print exactly one record per attempt: prefer the first "answered" (or error/closed if it ends early).
		if !st.printed {
			if res.Outcome == "answered" || res.Outcome == "error" || res.Outcome == "closed" {
				if data, err := json.Marshal(res); err == nil {
					fmt.Println(string(data))
				}
				if csvWriter != nil {
					rec := []string{
						res.AttemptID,
						res.CallID,
						res.PeerPhone,
						res.PeerURI,
						res.Direction,
						fmt.Sprintf("%v", res.ProtocolEnabled),
						fmt.Sprintf("%d", res.DialSentAtUnixMs),
						fmt.Sprintf("%d", res.AnsweredAtUnixMs),
						fmt.Sprintf("%d", res.LatencyMs),
						fmt.Sprintf("%d", res.ODAReqUnixMs),
						fmt.Sprintf("%d", res.ODADoneUnixMs),
						fmt.Sprintf("%d", res.ODALatencyMs),
						res.Outcome,
						res.Error,
					}
					if err := csvWriter.Write(rec); err != nil {
						return fmt.Errorf("writing csv record: %w", err)
					}
					csvWriter.Flush()
					if err := csvWriter.Error(); err != nil {
						return fmt.Errorf("flushing csv: %w", err)
					}
				}
				st.printed = true
			}
		}

		// Completion gating: don't start a new attempt until this one is actually closed.
		if res.Outcome == "closed" {
			st.closed = true
		}
		if res.Outcome == "error" {
			// No call to wait for.
			st.closed = true
		}

		if st.closed {
			mu.Lock()
			// Double-check it's still in-flight and then remove.
			if _, ok := inFlight[res.AttemptID]; ok {
				delete(inFlight, res.AttemptID)
			}
			mu.Unlock()
			completed++

			if started < runs {
				if err := startOne(); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
