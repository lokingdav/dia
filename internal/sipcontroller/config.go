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
	ODAAttributes []string

	// Incoming call behavior
	IncomingMode  string // baseline|integrated
	ODATimeoutSec int

	// Outgoing call behavior
	// If >= 0, triggers ODA for outgoing integrated calls after the call is
	// answered/established (and after RUA completes), waiting the specified
	// number of seconds.
	OutgoingODADelaySec int

	// Debug
	Verbose bool

	// Peer-session cache (Redis)
	// If CacheEnabled is true, the controller will try to reuse a cached DIA peer-session
	// to skip AKE (RUA-only). On cache miss, it falls back to the normal AKE+RUA flow.
	CacheEnabled bool
	RedisAddr    string
	RedisUser    string
	RedisPass    string
	RedisDB      int
	RedisPrefix  string
	// If >0, peer-session entries will be written with an expiration.
	PeerSessionTTLSeconds int

	// Experiments (optional, non-interactive)
	ExperimentMode                string
	ExperimentPhone               string
	ExperimentRuns                int
	ExperimentConcurrency         int
	ExperimentInterAttemptDelayMs int

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
	odaAttrs := flag.String("oda-attrs", "", "Comma-separated ODA attributes to request")
	flag.StringVar(&cfg.IncomingMode, "incoming-mode", "baseline", "Incoming call mode: baseline|integrated")
	flag.IntVar(&cfg.ODATimeoutSec, "oda-timeout", 10, "ODA timeout in seconds (automatic ODA flows)")
	flag.IntVar(&cfg.OutgoingODADelaySec, "outgoing-oda", -1, "If >=0, wait N seconds after outgoing call is answered/established (and RUA completes) then trigger ODA")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&cfg.CacheEnabled, "cache", false, "Enable Redis-backed DIA peer-session cache (skip AKE on cache hit)")
	flag.StringVar(&cfg.RedisAddr, "redis", "localhost:6379", "Redis address host:port for --cache")
	flag.StringVar(&cfg.RedisUser, "redis-user", "", "Redis username (optional)")
	flag.StringVar(&cfg.RedisPass, "redis-pass", "", "Redis password (optional)")
	flag.IntVar(&cfg.RedisDB, "redis-db", 0, "Redis DB number")
	flag.StringVar(&cfg.RedisPrefix, "redis-prefix", "denseid:dia:peer_session:v1", "Redis key prefix for peer session cache")
	flag.IntVar(&cfg.PeerSessionTTLSeconds, "peer-session-ttl", 0, "Peer session TTL in seconds (0 = no TTL)")
	flag.StringVar(&cfg.ExperimentMode, "experiment", "", "Run experiment mode: baseline|integrated")
	flag.StringVar(&cfg.ExperimentPhone, "phone", "", "Phone number/URI to dial for -experiment")
	flag.IntVar(&cfg.ExperimentRuns, "runs", 1, "Number of calls to place in -experiment")
	flag.IntVar(&cfg.ExperimentConcurrency, "concurrency", 1, "Max in-flight calls for -experiment")
	flag.IntVar(&cfg.ExperimentInterAttemptDelayMs, "inter-attempt-ms", 0, "Optional delay (ms) before starting the next experiment attempt after an attempt closes")
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
