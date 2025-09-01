package enrollment

import (
	"errors"

	"github.com/dense-identity/denseid/internal/signing"
)

type Config struct {
	Port                   string `env:"PORT" envDefault:":50051"`
	IsProduction           bool   `env:"IS_PRODUCTION" envDefault:"false"`
	EnrollmentDurationDays int    `env:"ENROLLMENT_DURATION_DAYS" envDefault:"30"`

	// Credential issuance keypair
	CiSkStr      string `env:"CI_SK,required"`
	CiPkStr      string `env:"CI_PK,required"`
	CiPrivateKey []byte
	CiPublicKey  []byte

	// Access Throttling keypair
	AtSkStr      string `env:"AT_SK,required"`
	AtPkStr      string `env:"AT_VK,required"`
	AtPrivateKey []byte
	AtPublicKey  []byte

	// Moderation public key
	AmfPkStr     string `env:"AMF_PK,required"`
	AmfPublicKey []byte
}

func (cfg *Config) ParseKeysAsBytes() error {
	if cfg == nil {
		return errors.New("failed to parse keys as bytes")
	}

	var err error

	cfg.CiPrivateKey, err = signing.DecodeHex(cfg.CiSkStr)
	if err != nil {
		return err
	}

	cfg.CiPublicKey, err = signing.DecodeHex(cfg.CiPkStr)
	if err != nil {
		return err
	}

	cfg.AtPrivateKey, err = signing.DecodeHex(cfg.AtSkStr)
	if err != nil {
		return err
	}

	cfg.AtPublicKey, err = signing.DecodeHex(cfg.AtPkStr)
	if err != nil {
		return err
	}

	cfg.AmfPublicKey, err = signing.DecodeHex(cfg.AmfPkStr)
	if err != nil {
		return err
	}

	return nil
}
