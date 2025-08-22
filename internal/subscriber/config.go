package subscriber

import (
	"errors"
	"github.com/dense-identity/denseid/internal/signing"
)

type Config struct {
	IsProduction           bool   `env:"IS_PRODUCTION" envDefault:"false"`
	RsHost           string   `env:"RS_HOST" envDefault:"localhost"`
	RsPort       int   `env:"RS_PORT" envDefault:"50054"`
	UseTls           bool   `env:"USE_TLS" envDefault:"false"`

	// Credential verification
	CiPkStr      string `env:"CI_PK,required"`
	CiPublicKey  []byte
	// Access Throttling keypair
	AtPkStr      string `env:"AT_VK,required"`
	AtPublicKey  []byte
	// Moderation public key
	AmfPkStr     string `env:"AMF_PK,required"`
	AmfPublicKey	[]byte
}

func (cfg *Config) ParseKeysAsBytes() error {
	if cfg == nil {
		return errors.New("failed to parse keys as bytes")
	}

	var err error

	cfg.CiPublicKey, err = signing.DecodeHex(cfg.CiPkStr)
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
