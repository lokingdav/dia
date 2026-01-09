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
	SelfPhone  string // extracted from DIAEnvFile (MY_PHONE)
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

	// Output
	OutputCSV string
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
	flag.StringVar(&cfg.OutputCSV, "csv", "", "Write experiment results to a CSV file")

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

// ExtractSelfPhoneFromDIAEnv returns MY_PHONE from an env-content string.
// Lines are expected to be in KEY=value format; empty lines and comments are ignored.
func ExtractSelfPhoneFromDIAEnv(envContent string) string {
	for _, line := range strings.Split(envContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "MY_PHONE" {
			return val
		}
	}
	return ""
}

// LoadDIAConfigAndSelfPhone loads DIA config and extracts MY_PHONE from the same env file.
func LoadDIAConfigAndSelfPhone(envFile string) (*dia.Config, string, error) {
	content, err := os.ReadFile(envFile)
	if err != nil {
		return nil, "", fmt.Errorf("reading env file: %w", err)
	}
	selfPhone := ExtractSelfPhoneFromDIAEnv(string(content))
	cfg, err := dia.ConfigFromEnv(string(content))
	if err != nil {
		return nil, "", err
	}
	return cfg, selfPhone, nil
}
