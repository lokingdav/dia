package revocation

import (
	"errors"

	"github.com/dense-identity/denseid/internal/signing"
)

type Config struct {
	Port         string `env:"PORT" envDefault:":50051"`
	IsProduction bool   `env:"IS_PRODUCTION" envDefault:"false"`

	GPKStr string `env:"GROUP_PK,required"`
	GPK    []byte
}

func (cfg *Config) ParseKeysAsBytes() error {
	if cfg == nil {
		return errors.New("failed to parse keys as bytes")
	}
	cfg.GPK, _ = signing.DecodeHex(cfg.GPKStr)
	return nil
}
