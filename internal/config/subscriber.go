package config

import (
	"errors"

	"github.com/dense-identity/denseid/internal/signing"
)

type SubscriberConfig struct {
	IsProduction bool `env:"IS_PRODUCTION" envDefault:"false"`
	UseTls       bool `env:"USE_TLS" envDefault:"false"`

	// Servers
	KeyServerAddr   string `env:"KEY_SERVER_ADDR,required"`
	RelayServerAddr string `env:"RELAY_SERVER_ADDR,required"`

	// Personal Details
	MyPhone string `env:"MY_PHONE,required"`
	MyName  string `env:"MY_NAME,required"`
	MyLogo  string `env:"MY_LOGO,required"`

	// Credential verification
	EnExpirationStr                        string `env:"ENROLLMENT_EXPIRATION,required"`
	RaPublicKeyStr                         string `env:"RA_PUBLIC_KEY,required"`
	RaSignatureStr                         string `env:"RA_SIGNATURE,required"`
	EnExpiration, RaPublicKey, RaSignature []byte

	// Right-To-Use
	RuaPrivateKeyStr            string `env:"RUA_PRIVATE_KEY,required"`
	RuaPublicKeyStr             string `env:"RUA_PUBLIC_KEY,required"`
	RuaPrivateKey, RuaPublicKey []byte

	// Access Ticket
	AccessTicketVkStr            string `env:"ACCESS_TICKET_VK,required"`
	SampleTicketStr              string `env:"SAMPLE_TICKET,required"`
	AccessTicketVk, SampleTicket []byte

	// Moderation public key
	ModeratorPublicKeyStr string `env:"MODERATOR_PUBLIC_KEY,required"`
	ModeratorPublicKey    []byte
}

func (conf *SubscriberConfig) ParseKeysAsBytes() error {
	if conf == nil {
		return errors.New("failed to parse keys as bytes")
	}

	var err error

	// RA credential verification
	conf.EnExpiration, err = signing.DecodeHex(conf.EnExpirationStr)
	if err != nil {
		return err
	}
	conf.RaPublicKey, err = signing.DecodeHex(conf.RaPublicKeyStr)
	if err != nil {
		return err
	}
	conf.RaSignature, err = signing.DecodeHex(conf.RaSignatureStr)
	if err != nil {
		return err
	}

	// Right-To-Use
	conf.RuaPrivateKey, err = signing.DecodeHex(conf.RuaPrivateKeyStr)
	if err != nil {
		return err
	}
	conf.RuaPublicKey, err = signing.DecodeHex(conf.RuaPublicKeyStr)
	if err != nil {
		return err
	}

	// Moderator
	conf.ModeratorPublicKey, err = signing.DecodeHex(conf.ModeratorPublicKeyStr)
	if err != nil {
		return err
	}

	// Access ticket
	conf.AccessTicketVk, err = signing.DecodeHex(conf.AccessTicketVkStr)
	if err != nil {
		return err
	}
	conf.SampleTicket, err = signing.DecodeHex(conf.SampleTicketStr)
	if err != nil {
		return err
	}

	return nil
}
