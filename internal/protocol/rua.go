package protocol

import (
	"errors"

	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
	dia "github.com/lokingdav/libdia/bindings/go"
)

// DeriveRuaTopic creates a new topic for RUA phase based on shared secret.
func DeriveRuaTopic(callState *CallState) []byte {
	message := helpers.HashAll(
		callState.SharedKey,
		[]byte(callState.Src),
		[]byte(callState.Dst),
		[]byte(callState.Ts),
	)
	return message
}

func InitRTU(callState *CallState, rtu *Rtu) error {
	ruaTopic := DeriveRuaTopic(callState)

	dhSk, dhPk, err := dia.DHKeygen()
	if err != nil {
		return err
	}

	callState.Rua = RuaState{
		Topic: ruaTopic,
		DhSk:  dhSk,
		DhPk:  dhPk,
		Rtu:   rtu,
	}

	return nil
}

// RuaRequest creates RuaRequest message
func RuaRequest(caller *CallState) ([]byte, error) {
	if caller == nil {
		return nil, errors.New("caller CallState cannot be nil")
	}

	InitRTU(caller, nil)

	ruaMsg := RuaMessage{
		Reason: caller.CallReason,
		DhPk:   helpers.EncodeToHex(caller.Rua.DhPk),
		Rtu:    *caller.Rua.Rtu,
	}

	// Sign message here

	msg, err := CreateRuaMessage(caller.SenderId, helpers.EncodeToHex(caller.Rua.Topic), TypeRuaRequest, &ruaMsg)
	if err != nil {
		return nil, err
	}

	ciphertext, err := encryption.SymEncrypt(caller.SharedKey, msg)
	if err != nil {
		return nil, err
	}

	return ciphertext, nil
}

