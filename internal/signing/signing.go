package signing

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
)

// KeyGen generates a new Ed25519 key pair.
// It returns the public key and the private key as byte slices.
// An error is returned if the cryptographic random number generator fails.
func KeyGen() (publicKey []byte, privateKey []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate ed25519 key pair: %w", err)
	}
	return pub, priv, nil
}

// Sign creates a cryptographic signature for a given message using a private key.
// It takes the private key and the message to be signed as byte slices.
// It returns the resulting signature as a byte slice.
func Sign(privateKey []byte, message []byte) []byte {
	return ed25519.Sign(privateKey, message)
}

// Verify checks if a signature is valid for a given message and public key.
// It returns true if the signature is valid, and false otherwise.
func Verify(publicKey []byte, message []byte, signature []byte) bool {
	return ed25519.Verify(publicKey, message, signature)
}

func DecodeString(v string) ([]byte, error) {
	return hex.DecodeString(v)
}

func EncodeToString(v []byte) (string) {
	return hex.EncodeToString(v)
}
