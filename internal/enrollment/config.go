package enrollment

import "errors"
import "github.com/dense-identity/denseid/internal/signing"

type Config struct {
	Port                   string `env:"PORT" envDefault:":50051"`
	IsProduction           bool   `env:"IS_PRODUCTION" envDefault:"false"`
	EnrollmentDurationDays int    `env:"ENROLLMENT_DURATION_DAYS" envDefault:"30"`

	PublicKeyStr  string `env:"PUBLIC_KEY,required"`
	PrivateKeyStr string `env:"PRIVATE_KEY,required"`

	PrivateKey []byte
	PublicKey  []byte

	// Group Signatures
	GPKStr string `env:"GROUP_PK,required"`
	ISKStr string `env:"GROUP_ISK,required"`
	OSKStr string `env:"GROUP_OSK,required"`

	GPK []byte
	OSK []byte
	ISK []byte
}

func (cfg *Config) ParseKeysAsBytes() error {
	if cfg == nil {
		return errors.New("failed to parse keys as bytes")
	}

	cfg.PrivateKey, _ = signing.DecodeString(cfg.PrivateKeyStr)
	cfg.GPK, _ = signing.DecodeString(cfg.GPKStr)
	cfg.ISK, _ = signing.DecodeString(cfg.ISKStr)
	cfg.OSK, _ = signing.DecodeString(cfg.OSKStr)

	return nil
}
