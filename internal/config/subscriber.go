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

	// AMF keys for RUA
	AmfPrivateKeyStr            string `env:"AMF_PRIVATE_KEY,required"`
	AmfPublicKeyStr             string `env:"AMF_PUBLIC_KEY,required"`
	AmfPrivateKey, AmfPublicKey []byte

	// PKE keys for encryption
	PkePrivateKeyStr            string `env:"PKE_PRIVATE_KEY,required"`
	PkePublicKeyStr             string `env:"PKE_PUBLIC_KEY,required"`
	PkePrivateKey, PkePublicKey []byte

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

	// AMF keys
	conf.AmfPrivateKey, err = signing.DecodeHex(conf.AmfPrivateKeyStr)
	if err != nil {
		return err
	}
	conf.AmfPublicKey, err = signing.DecodeHex(conf.AmfPublicKeyStr)
	if err != nil {
		return err
	}

	// PKE keys
	conf.PkePrivateKey, err = signing.DecodeHex(conf.PkePrivateKeyStr)
	if err != nil {
		return err
	}
	conf.PkePublicKey, err = signing.DecodeHex(conf.PkePublicKeyStr)
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
