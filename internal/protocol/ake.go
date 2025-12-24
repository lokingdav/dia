package protocol

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/dense-identity/denseid/internal/bbs"
	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
	dia "github.com/lokingdav/libdia/bindings/go"
)

func InitAke(callState *CallState) error {
	if callState == nil {
		return errors.New("nil CallState")
	}

	akeTopic := helpers.HashAll([]byte(callState.Src), []byte(callState.Ts))

	dhSk, dhPk, err := dia.DHKeygen()
	if err != nil {
		return err
	}

	callState.InitAke(dhSk, dhPk, akeTopic)
	return nil
}

func AkeRequest(caller *CallState) ([]byte, error) {
	if caller == nil {
		return nil, errors.New("caller CallState cannot be nil")
	}
	if len(caller.Ake.DhPk) == 0 {
		return nil, errors.New("AKE not initialized: DhPk is empty")
	}

	challenge := helpers.HashAll(caller.Ake.Topic)
	proof, err := CreateZKProof(caller, challenge)
	if err != nil {
		return nil, err
	}

	akeMsg := AkeMessage{
		PublicKey:  helpers.EncodeToHex(caller.Config.RuaPublicKey),
		Expiration: helpers.EncodeToHex(caller.Config.EnExpiration),
		Proof:      helpers.EncodeToHex(proof),
	}

	// Send on AKE topic
	msg, err := CreateAkeMessage(caller.SenderId, caller.GetAkeTopic(), TypeAkeRequest, &akeMsg)
	if err != nil {
		return nil, err
	}

	caller.UpdateCaller(challenge, proof)

	return msg, nil
}

func AkeResponse(recipient *CallState, callerMsg *ProtocolMessage) ([]byte, error) {
	if recipient == nil {
		return nil, errors.New("recipient CallState cannot be nil")
	}
	if callerMsg == nil {
		return nil, errors.New("caller ProtocolMessage cannot be nil")
	}
	if !callerMsg.IsAkeRequest() {
		return nil, errors.New("AkeResponse can only be called on AkeRequest message")
	}

	// Decode the AKE message from the protocol message
	var caller AkeMessage
	if err := callerMsg.DecodePayload(&caller); err != nil {
		return nil, fmt.Errorf("failed to decode AKE payload: %v", err)
	}

	challenge0 := helpers.HashAll(recipient.Ake.Topic)
	if !VerifyZKProof(&caller, recipient.Src, challenge0, recipient.Config.RaPublicKey) {
		return nil, errors.New("unauthenticated")
	}

	challenge1 := helpers.HashAll(caller.GetProof(), recipient.Ake.DhPk, challenge0)
	proof, err := CreateZKProof(recipient, challenge1)
	if err != nil {
		return nil, err
	}

	akeMsg := AkeMessage{
		DhPk:       helpers.EncodeToHex(recipient.Ake.DhPk),
		PublicKey:  helpers.EncodeToHex(recipient.Config.RuaPublicKey),
		Expiration: helpers.EncodeToHex(recipient.Config.EnExpiration),
		Proof:      helpers.EncodeToHex(proof),
	}

	// Store proofs for later use in AkeFinalize
	recipient.Ake.CallerProof = caller.GetProof()
	recipient.Ake.RecipientProof = proof

	// Respond on AKE topic
	msg, err := CreateAkeMessage(recipient.SenderId, recipient.GetAkeTopic(), TypeAkeResponse, &akeMsg)
	if err != nil {
		return nil, err
	}

	pk, err := helpers.DecodeHex(caller.PublicKey)

	if err != nil {
		return nil, err
	}

	ciphertext, err := encryption.PkeEncrypt(pk, msg)
	if err != nil {
		return nil, err
	}

	return ciphertext, nil
}

func AkeComplete(caller *CallState, recipientMsg *ProtocolMessage) ([]byte, error) {
	if caller == nil {
		return nil, errors.New("caller CallState cannot be nil")
	}
	if recipientMsg == nil {
		return nil, errors.New("recipient ProtocolMessage cannot be nil")
	}
	if !recipientMsg.IsAkeResponse() {
		return nil, errors.New("AkeComplete can only be called on AkeResponse message")
	}

	// Decode the AKE message from the protocol message
	var recipient AkeMessage
	if err := recipientMsg.DecodePayload(&recipient); err != nil {
		return nil, fmt.Errorf("failed to decode AKE payload: %v", err)
	}

	recipientDhPk, err1 := helpers.DecodeHex(recipient.DhPk)
	recipientProof, err2 := helpers.DecodeHex(recipient.Proof)
	if err1 != nil || err2 != nil {
		return nil, errors.New("something unexpected happened")
	}

	challenge := helpers.HashAll(caller.Ake.CallerProof, recipientDhPk, caller.Ake.Chal0)
	if !VerifyZKProof(&recipient, caller.Dst, challenge, caller.Config.RaPublicKey) {
		return nil, errors.New("unauthenticated")
	}

	secret, err := dia.DHComputeSecret(caller.Ake.DhSk, recipientDhPk)
	if err != nil {
		return nil, err
	}

	caller.SetSharedKey(ComputeSharedKey(
		caller.Ake.Topic,
		caller.Ake.CallerProof,
		recipientProof,
		caller.Ake.DhPk,
		recipientDhPk,
		secret,
	))

	akeMsg := AkeMessage{
		DhPk: helpers.EncodeToHex(helpers.ConcatBytes(caller.Ake.DhPk, recipientDhPk)),
	}

	msg, err := CreateAkeMessage(caller.SenderId, caller.GetAkeTopic(), TypeAkeComplete, &akeMsg)
	if err != nil {
		return nil, err
	}

	pk, err := helpers.DecodeHex(recipient.PublicKey)

	ciphertext, err := encryption.PkeEncrypt(pk, msg)
	if err != nil {
		return nil, err
	}

	return ciphertext, nil
}

func AkeFinalize(recipient *CallState, callerMsg *ProtocolMessage) error {
	if recipient == nil {
		return errors.New("recipient CallState cannot be nil")
	}
	if callerMsg == nil {
		return errors.New("caller ProtocolMessage cannot be nil")
	}
	if !callerMsg.IsAkeComplete() {
		return errors.New("AkeFinalize can only be called on AkeComplete message")
	}

	// Decode the AKE message from the protocol message
	var caller AkeMessage
	if err := callerMsg.DecodePayload(&caller); err != nil {
		return fmt.Errorf("failed to decode AKE payload: %v", err)
	}

	dhPk, err := helpers.DecodeHex(caller.DhPk)
	if err != nil {
		return err
	}

	if !bytes.Equal(dhPk[32:], recipient.Ake.DhPk) {
		return errors.New("Recipient DH PK do not match")
	}

	secret, err := dia.DHComputeSecret(recipient.Ake.DhSk, dhPk[:32])
	if err != nil {
		return err
	}

	recipient.SetSharedKey(ComputeSharedKey(
		recipient.Ake.Topic,
		recipient.Ake.CallerProof,
		recipient.Ake.RecipientProof,
		dhPk[:32],
		recipient.Ake.DhPk,
		secret,
	))

	return nil
}

func ComputeSharedKey(tpc, pieA, pieB, A, B, sec []byte) []byte {
	return helpers.HashAll(tpc, pieA, pieB, A, B, sec)
}

func CreateZKProof(prover *CallState, chal []byte) ([]byte, error) {
	var telephoneNumber string
	if prover.IamCaller() {
		telephoneNumber = prover.Src
	} else {
		telephoneNumber = prover.Dst
	}

	proof, err := bbs.ZkCreateProof(bbs.AkeZkProof{
		Tn:          telephoneNumber,
		Name:        prover.Config.MyName,
		PublicKey:   prover.Config.RuaPublicKey,
		Expiration:  prover.Config.EnExpiration,
		Nonce:       chal,
		RaPublicKey: prover.Config.RaPublicKey,
		Signature:   prover.Config.RaSignature,
	})
	if err != nil {
		return nil, err
	}
	return proof, nil
}

func VerifyZKProof(prover *AkeMessage, tn string, chal, raPublicKey []byte) bool {
	publicKey := prover.GetPublicKey()
	expiration := prover.GetExpiration()
	proof := prover.GetProof()

	ok, err := bbs.ZkVerifyProof(bbs.AkeZkProof{
		Tn:          tn,
		PublicKey:   publicKey,
		Expiration:  expiration,
		Nonce:       chal,
		RaPublicKey: raPublicKey,
		Proof:       proof,
	})
	if err != nil {
		return false
	}
	return ok
}
