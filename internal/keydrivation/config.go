package keyderivation

import (
	"errors"

	"github.com/dense-identity/denseid/internal/signing"
)

type Config struct {
	Port         string `env:"PORT" envDefault:":50051"`
	IsProduction bool   `env:"IS_PRODUCTION" envDefault:"false"`

	GPKStr string `env:"GROUP_PK,required"`
	GPK    []byte

	OprfSKStr string `env:"OPRF_SK,required"`
	OprfVKStr string `env:"OPRF_VK,required"`
	OprfSK    []byte
	OprfVK    []byte
}

func (cfg *Config) ParseKeysAsBytes() error {
	if cfg == nil {
		return errors.New("failed to parse keys as bytes")
	}

	cfg.GPK, _ = signing.DecodeString(cfg.GPKStr)
	cfg.OprfSK, _ = signing.DecodeString(cfg.OprfSKStr)
	cfg.OprfVK, _ = signing.DecodeString(cfg.OprfVKStr)

	return nil
}
