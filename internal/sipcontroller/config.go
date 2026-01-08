package sipcontroller

import (
	"flag"
	"fmt"
	"os"
	"strings"

	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

// Config holds all configuration for the SIP controller
type Config struct {
	// Baresip ctrl_tcp connection
	BaresipAddr string
	SipAccount  string

	// DIA configuration
	DIAEnvFile string
	RelayAddr  string
	RelayTLS   bool

	// Policy
	TimeoutSec    int
	AutoODA       bool
	ODAAttributes []string

	// Incoming call behavior
	IncomingMode   string // baseline|integrated
	ODAAfterAnswer bool
	ODATimeoutSec  int

	// Debug
	Verbose bool

	// Experiments (optional, non-interactive)
	ExperimentMode        string
	ExperimentPhone       string
	ExperimentRuns        int
	ExperimentConcurrency int
}

// ParseFlags parses command line flags and returns Config
func ParseFlags() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.SipAccount, "account", "", "User SIP account number")
	flag.StringVar(&cfg.BaresipAddr, "baresip", "localhost:4444", "Baresip ctrl_tcp address")
	flag.StringVar(&cfg.DIAEnvFile, "env", "", "Path to .env file with DIA credentials (required)")
	flag.StringVar(&cfg.RelayAddr, "relay", "localhost:50052", "DIA relay server address")
	flag.BoolVar(&cfg.RelayTLS, "relay-tls", false, "Use TLS for relay connection")
	flag.IntVar(&cfg.TimeoutSec, "timeout", 5, "DIA protocol timeout in seconds")
	flag.BoolVar(&cfg.AutoODA, "auto-oda", false, "Automatically trigger ODA after RUA")
	odaAttrs := flag.String("oda-attrs", "", "Comma-separated ODA attributes to request")
	flag.StringVar(&cfg.IncomingMode, "incoming-mode", "baseline", "Incoming call mode: baseline|integrated")
	flag.BoolVar(&cfg.ODAAfterAnswer, "oda-after-answer", false, "In integrated incoming mode, trigger ODA after CALL_ANSWERED")
	flag.IntVar(&cfg.ODATimeoutSec, "oda-timeout", 10, "ODA timeout in seconds (integrated incoming)")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")
	flag.StringVar(&cfg.ExperimentMode, "experiment", "", "Run experiment mode: baseline|integrated")
	flag.StringVar(&cfg.ExperimentPhone, "phone", "", "Phone number/URI to dial for -experiment")
	flag.IntVar(&cfg.ExperimentRuns, "runs", 1, "Number of calls to place in -experiment")
	flag.IntVar(&cfg.ExperimentConcurrency, "concurrency", 1, "Max in-flight calls for -experiment")

	flag.Parse()

	if cfg.DIAEnvFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -env flag is required")
		flag.Usage()
		os.Exit(1)
	}

	if *odaAttrs != "" {
		cfg.ODAAttributes = strings.Split(*odaAttrs, ",")
		for i, attr := range cfg.ODAAttributes {
			cfg.ODAAttributes[i] = strings.TrimSpace(attr)
		}
	}

	return cfg
}

// LoadDIAConfig loads DIA configuration from the env file
func LoadDIAConfig(envFile string) (*dia.Config, error) {
	content, err := os.ReadFile(envFile)
	if err != nil {
		return nil, fmt.Errorf("reading env file: %w", err)
	}
	return dia.ConfigFromEnv(string(content))
}
