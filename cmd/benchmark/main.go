package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

type runMode string

const (
	runBoth  runMode = "both"
	runPlain runMode = "plain"
	runRole  runMode = "role"
)

func writeOutput(path string, content string) error {
	if strings.TrimSpace(path) == "" {
		fmt.Print(content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Print("\n")
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func main() {
	var samples int
	var iters int
	var modeStr string
	var outPlain string
	var outRole string

	flag.IntVar(&samples, "samples", 30, "Number of samples per benchmark case (>= 1)")
	flag.IntVar(&iters, "iters", 0, "Override iteration count for each case (0 = use defaults)")
	flag.StringVar(&modeStr, "mode", string(runRole), "Which benchmark CSV to run: role|plain|both")
	flag.StringVar(&outPlain, "out-plain", "", "Write BenchProtocolCSV output to this file (default: stdout)")
	flag.StringVar(&outRole, "out-role", "", "Write BenchProtocolRoleCSV output to this file (default: stdout)")
	flag.Parse()

	if samples < 1 {
		log.Fatalf("-samples must be >= 1")
	}

	mode := runMode(strings.ToLower(strings.TrimSpace(modeStr)))
	switch mode {
	case runBoth, runPlain, runRole:
	default:
		log.Fatalf("invalid -mode %q (expected role|plain|both)", modeStr)
	}

	// Default to writing outputs to results/*.csv (overwriting each run)
	if (mode == runBoth || mode == runPlain) && strings.TrimSpace(outPlain) == "" {
		outPlain = filepath.Join("results", "primitives-pc.csv")
	}
	if (mode == runBoth || mode == runRole) && strings.TrimSpace(outRole) == "" {
		outRole = filepath.Join("results", "roles-pc.csv")
	}

	if mode == runBoth || mode == runPlain {
		csv, err := dia.BenchProtocolCSV(samples, iters)
		if err != nil {
			log.Fatalf("BenchProtocolCSV failed: %v", err)
		}
		if err := writeOutput(outPlain, csv); err != nil {
			log.Fatalf("writing plain output failed: %v", err)
		}
		log.Printf("[Benchmark] wrote %s", outPlain)
	}

	if mode == runBoth || mode == runRole {
		csv, err := dia.BenchProtocolRoleCSV(samples, iters)
		if err != nil {
			log.Fatalf("BenchProtocolRoleCSV failed: %v", err)
		}
		if err := writeOutput(outRole, csv); err != nil {
			log.Fatalf("writing role output failed: %v", err)
		}
		log.Printf("[Benchmark] wrote %s", outRole)
	}
}
