package protocol

import (
	"errors"

	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
)

// DeriveRuaTopic creates a new topic for RUA phase based on shared secret.
func DeriveRuaTopic(callState *CallState) string {
	message := helpers.HashAll(
		callState.SharedKey, 
		[]byte(callState.Src), 
		[]byte(callState.Dst),
		[]byte(callState.Ts),
	)
	return helpers.EncodeToHex(message)
}

// CreateRuaInitForCaller creates RuaInit message and transitions to RUA topic after AKE finalization.
func CreateRuaInitForCaller(caller *CallState) (string, []byte, error) {
	if caller == nil {
		return "", nil, errors.New("caller CallState cannot be nil")
	}

	// Derive and set RUA topic
	ruaTopic := DeriveRuaTopic(caller)
	caller.TransitionToRua(ruaTopic)

	ruaMsg := RuaMessage{
		Reason: caller.CallReason,
		DhPk: helpers.EncodeToHex(caller.DhPk),
		TnName: caller.Config.MyName,
		TnPublicKey:  helpers.EncodeToHex(caller.Config.RuaPublicKey),
		TnExp: helpers.EncodeToHex(caller.Config.EnExpiration),
		TnSig: helpers.EncodeToHex(caller.Config.RaSignature),
	}

	// Sign message here

	msg, err := CreateRuaMessage(caller.SenderId, ruaTopic, TypeRuaInit, &ruaMsg)
	if err != nil {
		return "", nil, err
	}

	ciphertext, err := encryption.SymEncrypt(caller.SharedKey, msg)
	if err != nil {
		return "", nil, err
	}

	return ruaTopic, ciphertext, nil
}
