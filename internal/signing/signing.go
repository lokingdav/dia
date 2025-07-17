package signing

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/hex"
	"fmt"

	"github.com/dense-identity/bbsgroupsig/bindings/go"
)

// EncodeToString encodes a given byte slice into hex string
func EncodeToString(v []byte) string {
	return hex.EncodeToString(v)
}

// DecodeString decodes a hex string into byte slice
func DecodeString(v string) ([]byte, error) {
	return hex.DecodeString(v)
}

// KeyGen generates a new Ed25519 key pair.
// It returns the public key and the private key as byte slices.
// An error is returned if the cryptographic random number generator fails.
func RegSigKeyGen() (publicKey []byte, privateKey []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate ed25519 key pair: %w", err)
	}
	return pub, priv, nil
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

// InitGroupSignatures inits pairing operations for group signatures
func InitGroupSignatures() {
	bbsgs.InitPairing()
}

// GrpSigUserKeyGen generates a unique user secret key given gpk and isk.
func GrpSigUserKeyGen(gpk, isk []byte) (usk []byte, err error) {
	usk, err = bbsgs.UserKeygen(gpk, isk)
	if err != nil {
		return nil, err
	}
	return usk, nil
}

// GrpSigSign signs a given message into a signature
func GrpSigSign(gpk, usk, msg []byte) ([]byte, error) {

	sig, err := bbsgs.Sign(gpk, usk, msg)
	if err != nil {
		return nil, err
	}
	return sig, nil
}

// GrpSigVerify verifies a given signature and message under the common gpk
func GrpSigVerify(gpk, sig, msg []byte) bool {
	return bbsgs.Verify(gpk, sig, msg)
}

// GrpSigOpenSig deanonymizes a given signture to reveal the signer
func GrpSigOpenSig(gpk, osk, sig []byte) ([]byte, error) {
	faulter, err := bbsgs.Open(gpk, osk, sig)
	if err != nil {
		return nil, err
	}
	return faulter, nil
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