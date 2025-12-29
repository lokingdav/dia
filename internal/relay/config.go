package relay

import (
	"errors"

	"github.com/dense-identity/denseid/internal/helpers"
)

type Config struct {
	Port         string `env:"PORT" envDefault:":50051"`
	IsProduction bool   `env:"IS_PRODUCTION" envDefault:"false"`

	AtVkStr     string `env:"AT_VK,required"`
	AtVerifyKey []byte

	// Redis configuration
	RedisAddr     string `env:"REDIS_ADDR" envDefault:"redis:6379"`
	RedisPassword string `env:"REDIS_PASSWORD" envDefault:""`
	RedisDB       int    `env:"REDIS_DB" envDefault:"0"`

	// Message TTL in seconds (default: 1 hour)
	MessageTTL int `env:"MESSAGE_TTL" envDefault:"5"`
}

func (cfg *Config) ParseKeysAsBytes() error {
	if cfg == nil {
		return errors.New("failed to parse keys as bytes")
	}

	var err error

	cfg.AtVerifyKey, err = helpers.DecodeHex(cfg.AtVkStr)
	if err != nil {
		return err
	}

	return nil
}
