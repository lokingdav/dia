package enrollment

import (
	"fmt"
	"os"
	"strings"

	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

type Config struct {
	Port                   string `env:"PORT" envDefault:":50051"`
	IsProduction           bool   `env:"IS_PRODUCTION" envDefault:"false"`
	EnrollmentDurationDays int    `env:"ENROLLMENT_DURATION_DAYS" envDefault:"30"`

	// DIA server configuration (manages all cryptographic keys)
	DiaServerConfig *dia.ServerConfig
}

// InitializeDiaServerConfig loads DIA server configuration from environment variables.
// Expected env vars: CI_SK, CI_PK, AT_SK, AT_VK, AMF_SK, AMF_PK
// Returns an error if any required key is missing.
func (cfg *Config) InitializeDiaServerConfig() error {
	// Build config from individual env vars
	envVars := []string{"CI_SK", "CI_PK", "AT_SK", "AT_VK", "AMF_SK", "AMF_PK"}
	envContent := strings.Builder{}

	var missing []string
	for _, key := range envVars {
		val := os.Getenv(key)
		if val == "" {
			missing = append(missing, key)
		} else {
			envContent.WriteString(fmt.Sprintf("%s=%s\n", key, val))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required DIA server config env vars: %s", strings.Join(missing, ", "))
	}

	var err error
	cfg.DiaServerConfig, err = dia.ServerConfigFromEnv(envContent.String())
	if err != nil {
		return fmt.Errorf("failed to parse DIA keys from env: %w", err)
	}
	return nil
}
