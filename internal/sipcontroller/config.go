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

	// DIA configuration
	DIAEnvFile string
	RelayAddr  string
	RelayTLS   bool

	// Policy
	TimeoutSec    int
	AutoODA       bool
	ODAAttributes []string

	// Debug
	Verbose bool
}

// ParseFlags parses command line flags and returns Config
func ParseFlags() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.BaresipAddr, "baresip", "localhost:4444", "Baresip ctrl_tcp address")
	flag.StringVar(&cfg.DIAEnvFile, "env", "", "Path to .env file with DIA credentials (required)")
	flag.StringVar(&cfg.RelayAddr, "relay", "localhost:50052", "DIA relay server address")
	flag.BoolVar(&cfg.RelayTLS, "relay-tls", false, "Use TLS for relay connection")
	flag.IntVar(&cfg.TimeoutSec, "timeout", 5, "DIA protocol timeout in seconds")
	flag.BoolVar(&cfg.AutoODA, "auto-oda", false, "Automatically trigger ODA after RUA")
	odaAttrs := flag.String("oda-attrs", "", "Comma-separated ODA attributes to request")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")

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
