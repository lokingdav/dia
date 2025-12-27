package encryption

import (
	"bytes"
	"testing"
)

func TestPkeKeygen(t *testing.T) {
	privateKey, publicKey, err := PkeKeygen()
	if err != nil {
		t.Fatalf("PkeKeygen failed: %v", err)
	}

	if len(privateKey) != 32 {
		t.Errorf("expected private key length 32, got %d", len(privateKey))
	}

	if len(publicKey) != 32 {
		t.Errorf("expected public key length 32, got %d", len(publicKey))
	}

	// Generate another keypair to ensure randomness
	privateKey2, publicKey2, err := PkeKeygen()
	if err != nil {
		t.Fatalf("second PkeKeygen failed: %v", err)
	}

	if bytes.Equal(privateKey, privateKey2) {
		t.Error("two generated private keys should not be equal")
	}

	if bytes.Equal(publicKey, publicKey2) {
		t.Error("two generated public keys should not be equal")
	}
}

func TestPkeEncryptDecrypt(t *testing.T) {
	privateKey, publicKey, err := PkeKeygen()
	if err != nil {
		t.Fatalf("PkeKeygen failed: %v", err)
	}

	plaintext := []byte("Hello, this is a secret message for testing PKE encryption!")

	ciphertext, err := PkeEncrypt(publicKey, plaintext)
	if err != nil {
		t.Fatalf("PkeEncrypt failed: %v", err)
	}

	expectedMinLen := 32 + 12 + len(plaintext) + 16
	if len(ciphertext) < expectedMinLen {
		t.Errorf("ciphertext too short: expected at least %d, got %d", expectedMinLen, len(ciphertext))
	}

	decrypted, err := PkeDecrypt(privateKey, ciphertext)
	if err != nil {
		t.Fatalf("PkeDecrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted text does not match original")
	}
}

func TestPkeEncryptDecryptEmptyMessage(t *testing.T) {
	_, publicKey, err := PkeKeygen()
	if err != nil {
		t.Fatalf("PkeKeygen failed: %v", err)
	}

	_, err = PkeEncrypt(publicKey, []byte{})
	if err == nil {
		t.Error("expected error for empty plaintext")
	}
}

func TestPkeEncryptDecryptLargeMessage(t *testing.T) {
	privateKey, publicKey, err := PkeKeygen()
	if err != nil {
		t.Fatalf("PkeKeygen failed: %v", err)
	}

	plaintext := make([]byte, 1024*1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	ciphertext, err := PkeEncrypt(publicKey, plaintext)
	if err != nil {
		t.Fatalf("PkeEncrypt failed for large message: %v", err)
	}

	decrypted, err := PkeDecrypt(privateKey, ciphertext)
	if err != nil {
		t.Fatalf("PkeDecrypt failed for large message: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("decrypted large message does not match original")
	}
}

func TestPkeDecryptWithWrongKey(t *testing.T) {
	_, publicKey, err := PkeKeygen()
	if err != nil {
		t.Fatalf("PkeKeygen failed: %v", err)
	}

	wrongPrivateKey, _, err := PkeKeygen()
	if err != nil {
		t.Fatalf("second PkeKeygen failed: %v", err)
	}

	plaintext := []byte("Secret message")

	ciphertext, err := PkeEncrypt(publicKey, plaintext)
	if err != nil {
		t.Fatalf("PkeEncrypt failed: %v", err)
	}

	_, err = PkeDecrypt(wrongPrivateKey, ciphertext)
	if err == nil {
		t.Error("expected decryption to fail with wrong key")
	}
}

func TestPkeDecryptTamperedCiphertext(t *testing.T) {
	privateKey, publicKey, err := PkeKeygen()
	if err != nil {
		t.Fatalf("PkeKeygen failed: %v", err)
	}

	plaintext := []byte("Secret message")

	ciphertext, err := PkeEncrypt(publicKey, plaintext)
	if err != nil {
		t.Fatalf("PkeEncrypt failed: %v", err)
	}

	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-10] ^= 0xFF

	_, err = PkeDecrypt(privateKey, tampered)
	if err == nil {
		t.Error("expected decryption to fail with tampered ciphertext")
	}
}

func TestPkeInvalidKeySize(t *testing.T) {
	t.Run("InvalidPublicKeySize", func(t *testing.T) {
		invalidPubKey := make([]byte, 16)
		plaintext := []byte("test")

		_, err := PkeEncrypt(invalidPubKey, plaintext)
		if err == nil {
			t.Error("expected error for invalid public key size")
		}
	})

	t.Run("InvalidPrivateKeySize", func(t *testing.T) {
		_, publicKey, err := PkeKeygen()
		if err != nil {
			t.Fatalf("PkeKeygen failed: %v", err)
		}

		ciphertext, err := PkeEncrypt(publicKey, []byte("test"))
		if err != nil {
			t.Fatalf("PkeEncrypt failed: %v", err)
		}

		invalidPrivKey := make([]byte, 16)
		_, err = PkeDecrypt(invalidPrivKey, ciphertext)
		if err == nil {
			t.Error("expected error for invalid private key size")
		}
	})

	t.Run("CiphertextTooShort", func(t *testing.T) {
		privateKey, _, err := PkeKeygen()
		if err != nil {
			t.Fatalf("PkeKeygen failed: %v", err)
		}

		shortCiphertext := make([]byte, 10)
		_, err = PkeDecrypt(privateKey, shortCiphertext)
		if err == nil {
			t.Error("expected error for short ciphertext")
		}
	})
}

func TestPkeMultipleEncryptionsProduceDifferentCiphertexts(t *testing.T) {
	_, publicKey, err := PkeKeygen()
	if err != nil {
		t.Fatalf("PkeKeygen failed: %v", err)
	}

	plaintext := []byte("Same message encrypted multiple times")

	ciphertext1, err := PkeEncrypt(publicKey, plaintext)
	if err != nil {
		t.Fatalf("first PkeEncrypt failed: %v", err)
	}

	ciphertext2, err := PkeEncrypt(publicKey, plaintext)
	if err != nil {
		t.Fatalf("second PkeEncrypt failed: %v", err)
	}

	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Error("encrypting same plaintext twice should produce different ciphertexts")
	}
}

func TestPkeCrossPartyEncryption(t *testing.T) {
	alicePrivate, alicePublic, err := PkeKeygen()
	if err != nil {
		t.Fatalf("Alice PkeKeygen failed: %v", err)
	}

	bobPrivate, bobPublic, err := PkeKeygen()
	if err != nil {
		t.Fatalf("Bob PkeKeygen failed: %v", err)
	}

	messageFromAlice := []byte("Hello Bob, this is Alice!")
	ciphertext, err := PkeEncrypt(bobPublic, messageFromAlice)
	if err != nil {
		t.Fatalf("Alice's PkeEncrypt failed: %v", err)
	}

	decrypted, err := PkeDecrypt(bobPrivate, ciphertext)
	if err != nil {
		t.Fatalf("Bob's PkeDecrypt failed: %v", err)
	}

	if !bytes.Equal(messageFromAlice, decrypted) {
		t.Error("Bob did not receive Alice's message correctly")
	}

	messageFromBob := []byte("Hi Alice, Bob here!")
	ciphertext2, err := PkeEncrypt(alicePublic, messageFromBob)
	if err != nil {
		t.Fatalf("Bob's PkeEncrypt failed: %v", err)
	}

	decrypted2, err := PkeDecrypt(alicePrivate, ciphertext2)
	if err != nil {
		t.Fatalf("Alice's PkeDecrypt failed: %v", err)
	}

	if !bytes.Equal(messageFromBob, decrypted2) {
		t.Error("Alice did not receive Bob's message correctly")
	}
}
