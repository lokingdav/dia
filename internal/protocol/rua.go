package protocol

import (
	"errors"

	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
)

// DeriveRtuTopic creates a new topic for RTU phase based on shared secret.
func DeriveRtuTopic(sharedSecret []byte) string {
	return helpers.Hash256Hex(helpers.ConcatBytes(sharedSecret, []byte("2")))
}

// CreateRtuInitForCaller creates RtuInit message and transitions to RTU topic after AKE finalization.
func CreateRtuInitForCaller(caller *CallState) (string, []byte, error) {
	if caller == nil {
		return "", nil, errors.New("caller CallState cannot be nil")
	}

	// Derive and set RTU topic
	rtuTopic := DeriveRtuTopic(caller.SharedKey)
	caller.TransitionToRtu(rtuTopic)

	// RTU init (re-uses AKE message structure for now)
	c0 := helpers.Hash256(helpers.ConcatBytes(caller.SharedKey, caller.DhPk, caller.GetAkeLabel()))
	proof, err := CreateZKProof(caller, c0)
	if err != nil {
		return "", nil, err
	}

	rtuMsg := AkeMessage{
		DhPk:       helpers.EncodeToHex(caller.DhPk),
		PublicKey:  helpers.EncodeToHex(caller.Config.RtuPublicKey),
		Expiration: helpers.EncodeToHex(caller.Config.EnExpiration),
		Proof:      helpers.EncodeToHex(proof),
	}

	msg, err := CreateRtuInitMessage(caller.SenderId, rtuTopic, &rtuMsg)
	if err != nil {
		return "", nil, err
	}

	ciphertext, err := encryption.SymEncrypt(caller.SharedKey, msg)
	if err != nil {
		return "", nil, err
	}

	return rtuTopic, ciphertext, nil
}