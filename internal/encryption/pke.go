package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	// X25519 key sizes
	x25519PrivateKeySize = 32
	x25519PublicKeySize  = 32
	// AES-256-GCM parameters
	aesKeySize   = 32
	aesNonceSize = 12
)

// PkeEncrypt encrypts plaintext using ECIES with X25519 and AES-256-GCM.
// The publicKey should be a 32-byte X25519 public key.
func PkeEncrypt(publicKey, plaintext []byte) ([]byte, error) {
	if len(publicKey) != x25519PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: expected %d, got %d", x25519PublicKeySize, len(publicKey))
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("plaintext cannot be empty")
	}

	// Generate ephemeral keypair
	ephemeralPrivate := make([]byte, x25519PrivateKeySize)
	if _, err := io.ReadFull(rand.Reader, ephemeralPrivate); err != nil {
		return nil, fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	// Compute ephemeral public key
	ephemeralPublic, err := curve25519.X25519(ephemeralPrivate, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("failed to compute ephemeral public key: %w", err)
	}

	// Compute shared secret using ECDH
	sharedSecret, err := curve25519.X25519(ephemeralPrivate, publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shared secret: %w", err)
	}

	// Derive AES key using HKDF
	aesKey, err := deriveKey(sharedSecret, ephemeralPublic, publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	// Encrypt with AES-256-GCM
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, aesNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate
	ciphertext := gcm.Seal(nil, nonce, plaintext, ephemeralPublic)

	// Output format: ephemeralPublic (32) || nonce (12) || ciphertext+tag
	result := make([]byte, 0, x25519PublicKeySize+aesNonceSize+len(ciphertext))
	result = append(result, ephemeralPublic...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// PkeDecrypt decrypts ciphertext using ECIES with X25519 and AES-256-GCM.
// The privateKey should be a 32-byte X25519 private key.
func PkeDecrypt(privateKey, ciphertext []byte) ([]byte, error) {
	if len(privateKey) != x25519PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: expected %d, got %d", x25519PrivateKeySize, len(privateKey))
	}

	minCiphertextSize := x25519PublicKeySize + aesNonceSize + 16 // 16 is GCM tag size
	if len(ciphertext) < minCiphertextSize {
		return nil, fmt.Errorf("ciphertext too short: minimum %d bytes required", minCiphertextSize)
	}

	// Parse ciphertext components
	ephemeralPublic := ciphertext[:x25519PublicKeySize]
	nonce := ciphertext[x25519PublicKeySize : x25519PublicKeySize+aesNonceSize]
	encryptedData := ciphertext[x25519PublicKeySize+aesNonceSize:]

	// Compute our public key for key derivation
	myPublicKey, err := curve25519.X25519(privateKey, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("failed to compute public key: %w", err)
	}

	// Compute shared secret using ECDH
	sharedSecret, err := curve25519.X25519(privateKey, ephemeralPublic)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shared secret: %w", err)
	}

	// Derive AES key using HKDF
	aesKey, err := deriveKey(sharedSecret, ephemeralPublic, myPublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	// Decrypt with AES-256-GCM
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt and verify
	plaintext, err := gcm.Open(nil, nonce, encryptedData, ephemeralPublic)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// deriveKey derives an AES key from the shared secret using HKDF-SHA256
func deriveKey(sharedSecret, ephemeralPublic, recipientPublic []byte) ([]byte, error) {
	// Create info for HKDF: ephemeral_pk || recipient_pk
	info := make([]byte, 0, len(ephemeralPublic)+len(recipientPublic))
	info = append(info, ephemeralPublic...)
	info = append(info, recipientPublic...)

	// HKDF-SHA256
	hkdfReader := hkdf.New(sha256.New, sharedSecret, nil, info)
	key := make([]byte, aesKeySize)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, err
	}

	return key, nil
}

// PkeKeygen generates a new X25519 keypair for PKE.
// Returns (privateKey, publicKey, error) where both are 32-byte keys.
func PkeKeygen() (privateKey, publicKey []byte, err error) {
	privateKey = make([]byte, x25519PrivateKeySize)
	if _, err := io.ReadFull(rand.Reader, privateKey); err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	publicKey, err = curve25519.X25519(privateKey, curve25519.Basepoint)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compute public key: %w", err)
	}

	return privateKey, publicKey, nil
}
