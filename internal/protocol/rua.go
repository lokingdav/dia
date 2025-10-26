package protocol

import (
	"errors"

	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
)

// DeriveRuaTopic creates a new topic for RUA phase based on shared secret.
func DeriveRuaTopic(sharedSecret []byte) string {
	return helpers.Hash256Hex(helpers.ConcatBytes(sharedSecret, []byte("2")))
}

// CreateRuaInitForCaller creates RuaInit message and transitions to RUA topic after AKE finalization.
func CreateRuaInitForCaller(caller *CallState) (string, []byte, error) {
	if caller == nil {
		return "", nil, errors.New("caller CallState cannot be nil")
	}

	// Derive and set RUA topic
	ruaTopic := DeriveRuaTopic(caller.SharedKey)
	caller.TransitionToRua(ruaTopic)

	// RUA init (re-uses AKE message structure for now)
	c0 := helpers.Hash256(helpers.ConcatBytes(caller.SharedKey, caller.DhPk, caller.GetAkeLabel()))
	proof, err := CreateZKProof(caller, c0)
	if err != nil {
		return "", nil, err
	}

	ruaMsg := AkeMessage{
		DhPk:       helpers.EncodeToHex(caller.DhPk),
		PublicKey:  helpers.EncodeToHex(caller.Config.RuaPublicKey),
		Expiration: helpers.EncodeToHex(caller.Config.EnExpiration),
		Proof:      helpers.EncodeToHex(proof),
	}

	msg, err := CreateRuaInitMessage(caller.SenderId, ruaTopic, &ruaMsg)
	if err != nil {
		return "", nil, err
	}

	ciphertext, err := encryption.SymEncrypt(caller.SharedKey, msg)
	if err != nil {
		return "", nil, err
	}

	return ruaTopic, ciphertext, nil
}
