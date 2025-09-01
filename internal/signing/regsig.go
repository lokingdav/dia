package signing

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/hex"
	"fmt"
)

// EncodeToHex encodes a given byte slice into hex string
func EncodeToHex(v []byte) string {
	return hex.EncodeToString(v)
}

// DecodeHex decodes a hex string into byte slice
func DecodeHex(v string) ([]byte, error) {
	return hex.DecodeString(v)
}

// KeyGen generates a new Ed25519 key pair.
// It returns the public key and the private key as byte slices.
// An error is returned if the cryptographic random number generator fails.
func RegSigKeyGen() (privateKey []byte, publicKey []byte, err error) {
	publicKey, privateKey, err = ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate ed25519 key pair: %w", err)
	}
	return privateKey, publicKey, nil
}

// Sign creates a cryptographic signature for a given message using a private key.
// It takes the private key and the message to be signed as byte slices.
// It returns the resulting signature as a byte slice.
func RegSigSign(privateKey []byte, message []byte) []byte {
	return ed25519.Sign(privateKey, message)
}

// Verify checks if a signature is valid for a given message and public key.
// It returns true if the signature is valid, and false otherwise.
func RegSigVerify(publicKey []byte, message []byte, signature []byte) bool {
	return ed25519.Verify(publicKey, message, signature)
}

func ImportPublicKeyFromDER(der []byte) (ed25519.PublicKey, error) {
	pubIfc, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := pubIfc.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an Ed25519 public key")
	}
	return pub, nil
}

func ExportPublicKeyToDER(publicKey ed25519.PublicKey) ([]byte, error) {
	pk, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("exportPublicKeyToDER:: %v", err)
	}
	return pk, nil
}
