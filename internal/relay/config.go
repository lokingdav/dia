package relay

import (
	"errors"

	"github.com/dense-identity/denseid/internal/signing"
)

type Config struct {
	Port         string `env:"PORT" envDefault:":50051"`
	IsProduction bool   `env:"IS_PRODUCTION" envDefault:"false"`

	AtVkStr string `env:"AT_VK,required"`
	AtVerifyKey    []byte
}

func (cfg *Config) ParseKeysAsBytes() error {
	if cfg == nil {
		return errors.New("failed to parse keys as bytes")
	}
	
	var err error

	cfg.AtVerifyKey, err = signing.DecodeHex(cfg.AtVkStr)
	if err != nil {
		return err
	}

	return nil
}
