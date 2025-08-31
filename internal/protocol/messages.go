package protocol

import (
	"encoding/json"

	"github.com/dense-identity/denseid/internal/helpers"
)

type AkeMessage1 struct {
	DhPk, Ciphertext, ZkProof []byte
}

type AkeMessage2 struct {
	AkeMessage1
}

func (m1 *AkeMessage1) VerifyZk() bool {
	return false
}

func (m1 *AkeMessage1) Encrypt(secretKey []byte) ([]byte, error) {
	plaintext, err := json.Marshal(map[string]any{
		"dhPk":  helpers.EncodeToHex(m1.DhPk),
		"proof": helpers.EncodeToHex(m1.ZkProof),
	})
	if err != nil {
		return nil, err
	}
	// todo encrypt
	ctx := plaintext

	return ctx, nil
}

func (m1 *AkeMessage1) Decrypt(secretKey []byte) error {
	return nil
}

func (m2 *AkeMessage2) Encrypt(secretKey []byte) error {
	return nil
}

func (m2 *AkeMessage2) Decrypt(privateKey []byte) error {
	return nil
}
