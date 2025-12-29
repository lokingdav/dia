package enrollment

import (
	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

type Config struct {
	Port                   string `env:"PORT" envDefault:":50051"`
	IsProduction           bool   `env:"IS_PRODUCTION" envDefault:"false"`
	EnrollmentDurationDays int    `env:"ENROLLMENT_DURATION_DAYS" envDefault:"30"`

	// DIA server configuration (manages all cryptographic keys)
	DiaServerConfig *dia.ServerConfig
}

// InitializeDiaServerConfig creates a new DIA server configuration.
// This should be called once during server initialization.
func (cfg *Config) InitializeDiaServerConfig() error {
	var err error
	cfg.DiaServerConfig, err = dia.GenerateServerConfig(cfg.EnrollmentDurationDays)
	return err
}
