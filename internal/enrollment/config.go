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
	PublicKeyDER []byte

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

	var err error

	cfg.PrivateKey, err = signing.DecodeString(cfg.PrivateKeyStr)
	if err != nil {
		return err
	}

	cfg.PublicKey, err = signing.DecodeString(cfg.PublicKeyStr)
	if err != nil {
		return err
	}
	
	cfg.PublicKeyDER, err = signing.ExportPublicKeyToDER(cfg.PublicKey)
	if err != nil {
		return err
	}

	cfg.GPK, err = signing.DecodeString(cfg.GPKStr)
	if err != nil {
		return err
	}

	cfg.ISK, err = signing.DecodeString(cfg.ISKStr)
	if err != nil {
		return err
	}

	cfg.OSK, err = signing.DecodeString(cfg.OSKStr)
	if err != nil {
		return err
	}

	return nil
}
