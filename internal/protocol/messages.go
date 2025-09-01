package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/dense-identity/denseid/internal/helpers"
)

type AkeMessage1 struct {
	DhPk, Ciphertext, ZkProof []byte
}

type AkeM1Payload struct {
	Type  string `json:"type"`
	DhPk  string `json:"dhPk"`
	Proof string `json:"proof"`
}

type AkeMessage2 struct {
	AkeMessage1
}

func (m1 *AkeMessage1) VerifyZk() bool {
	return false
}

func (m1 *AkeMessage1) Encrypt(secretKey []byte) ([]byte, error) {
	plaintext, err := json.Marshal(AkeM1Payload{
		Type:  "AkeM1",
		DhPk:  helpers.EncodeToHex(m1.DhPk),
		Proof: helpers.EncodeToHex(m1.ZkProof),
	})

	if err != nil {
		return nil, err
	}

	// todo encrypt
	ctx := plaintext

	return ctx, nil
}

func (m1 *AkeMessage1) Decrypt(secretKey []byte) error {
	// TODO: plaintext := decrypt(ctx, secretKey)
	plaintext := m1.Ciphertext

	var p AkeM1Payload
	if err := json.Unmarshal(plaintext, &p); err != nil {
		return fmt.Errorf("unmarshal m1 payload: %w", err)
	}

	dhPk, err := helpers.DecodeHex(p.DhPk)
	if err != nil {
		return fmt.Errorf("decode dhPk: %w", err)
	}
	proof, err := helpers.DecodeHex(p.Proof)
	if err != nil {
		return fmt.Errorf("decode proof: %w", err)
	}

	m1.DhPk = dhPk
	m1.ZkProof = proof
	return nil
}

func (m2 *AkeMessage2) Encrypt(secretKey []byte) error {
	return nil
}

func (m2 *AkeMessage2) Decrypt(privateKey []byte) error {
	return nil
}
