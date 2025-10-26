package protocol

import (
	"context"
	"errors"
	"fmt"
	"time"

	keypb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	"github.com/dense-identity/denseid/internal/bbs"
	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
	"github.com/dense-identity/denseid/internal/voprf"
	dia "github.com/lokingdav/libdia/bindings/go"
)

// AkeDeriveKey performs the VOPRF round-trip and returns a derived key.
func AkeDeriveKey(ctx context.Context, client keypb.KeyDerivationServiceClient, callState *CallState) ([]byte, error) {
	if client == nil {
		return nil, errors.New("nil KeyDerivationServiceClient")
	}
	if callState == nil {
		return nil, errors.New("nil CallState")
	}

	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	blinded, blind, err := voprf.Blind(callState.GetAkeLabel())
	if err != nil {
		return nil, err
	}

	resp, err := client.Evaluate(cctx, &keypb.EvaluateRequest{
		BlindedElement: blinded,
		Ticket:         callState.Ticket,
	})
	if err != nil {
		return nil, err
	}
	eval := resp.GetEvaluatedElement()
	if len(eval) == 0 {
		return nil, errors.New("empty evaluated element")
	}

	out, err := voprf.Finalize(eval, blind)
	helpers.WipeBytes(blind)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func InitAke(callState *CallState) error {
	if callState == nil {
		return errors.New("nil CallState")
	}

	akeTopic := helpers.Hash256Hex(helpers.ConcatBytes(callState.SharedKey, []byte("1")))

	dhSk, dhPk, err := dia.DHKeygen()
	if err != nil {
		return err
	}

	callState.InitAke(dhSk, dhPk, akeTopic)
	return nil
}

func AkeInitCallerToRecipient(caller *CallState) ([]byte, error) {
	if caller == nil {
		return nil, errors.New("caller CallState cannot be nil")
	}
	if len(caller.DhPk) == 0 {
		return nil, errors.New("AKE not initialized: DhPk is empty")
	}

	c0 := helpers.Hash256(helpers.ConcatBytes(caller.SharedKey, caller.DhPk, caller.GetAkeLabel()))
	proof, err := CreateZKProof(caller, c0)
	if err != nil {
		return nil, err
	}

	akeMsg := AkeMessage{
		DhPk:       helpers.EncodeToHex(caller.DhPk),
		PublicKey:  helpers.EncodeToHex(caller.Config.RtuPublicKey),
		Expiration: helpers.EncodeToHex(caller.Config.EnExpiration),
		Proof:      helpers.EncodeToHex(proof),
	}

	// Send on AKE topic
	msg, err := CreateAkeInitMessage(caller.SenderId, caller.AkeTopic, &akeMsg)
	if err != nil {
		return nil, err
	}

	caller.UpdateR1(c0, proof)
	return encryption.SymEncrypt(caller.SharedKey, msg)
}

func AkeResponseRecipientToCaller(recipient *CallState, callerMsg *ProtocolMessage) ([]byte, error) {
	if recipient == nil {
		return nil, errors.New("recipient CallState cannot be nil")
	}
	if callerMsg == nil {
		return nil, errors.New("caller ProtocolMessage cannot be nil")
	}
	if !callerMsg.IsAkeInit() {
		return nil, errors.New("AkeResponseRecipientToCaller can only be called on AkeInit message")
	}

	// Decode the AKE message from the protocol message
	var caller AkeMessage
	if err := callerMsg.DecodePayload(&caller); err != nil {
		return nil, fmt.Errorf("failed to decode AKE payload: %v", err)
	}

	callerDhPk := caller.GetDhPk()
	callerProof := caller.GetProof()

	c0 := helpers.Hash256(helpers.ConcatBytes(recipient.SharedKey, callerDhPk, recipient.GetAkeLabel()))
	if !VerifyZKProof(&caller, recipient.CallerId, c0, recipient.Config.RaPublicKey) {
		return nil, errors.New("unauthenticated")
	}

	c1 := helpers.Hash256(helpers.ConcatBytes(callerProof, callerDhPk, recipient.DhPk, c0))
	proof, err := CreateZKProof(recipient, c1)
	if err != nil {
		return nil, err
	}

	akeMsg := AkeMessage{
		DhPk:       helpers.EncodeToHex(recipient.DhPk),
		PublicKey:  helpers.EncodeToHex(recipient.Config.RtuPublicKey),
		Expiration: helpers.EncodeToHex(recipient.Config.EnExpiration),
		Proof:      helpers.EncodeToHex(proof),
	}

	// Respond on AKE topic
	msg, err := CreateAkeResponseMessage(recipient.SenderId, recipient.AkeTopic, &akeMsg)
	if err != nil {
		return nil, err
	}

	ciphertext, err := encryption.SymEncrypt(recipient.SharedKey, msg)
	if err != nil {
		return nil, err
	}

	secret, err := dia.DHComputeSecret(recipient.DhSk, callerDhPk)
	if err != nil {
		return nil, err
	}

	recipient.SetSharedKey(ComputeSharedKey(
		recipient.SharedKey,
		recipient.MarshalAkeTopic(), // AKE topic binds the transcript
		callerProof,
		proof,
		callerDhPk,
		recipient.DhPk,
		c0,
		c1,
		secret,
	))

	return ciphertext, nil
}

func AkeFinalizeCaller(caller *CallState, recipientMsg *ProtocolMessage) error {
	if caller == nil {
		return errors.New("caller CallState cannot be nil")
	}
	if recipientMsg == nil {
		return errors.New("recipient ProtocolMessage cannot be nil")
	}
	if !recipientMsg.IsAkeResponse() {
		return errors.New("AkeFinalizeCaller can only be called on AkeResponse message")
	}

	// Decode the AKE message from the protocol message
	var recipient AkeMessage
	if err := recipientMsg.DecodePayload(&recipient); err != nil {
		return fmt.Errorf("failed to decode AKE payload: %v", err)
	}

	recipientDhPk, err1 := helpers.DecodeHex(recipient.DhPk)
	recipientProof, err2 := helpers.DecodeHex(recipient.Proof)
	if err1 != nil || err2 != nil {
		return errors.New("something unexpected happened")
	}

	c1 := helpers.Hash256(helpers.ConcatBytes(caller.Proof, caller.DhPk, recipientDhPk, caller.Chal0))
	if !VerifyZKProof(&recipient, caller.Recipient, c1, caller.Config.RaPublicKey) {
		return errors.New("unauthenticated")
	}

	secret, err := dia.DHComputeSecret(caller.DhSk, recipientDhPk)
	if err != nil {
		return err
	}

	caller.SetSharedKey(ComputeSharedKey(
		caller.SharedKey,
		caller.MarshalAkeTopic(), // AKE topic binds the transcript
		caller.Proof,
		recipientProof,
		caller.DhPk,
		recipientDhPk,
		caller.Chal0,
		c1,
		secret,
	))

	return nil
}

// AkeCompleteSendToRecipient sends the AkeComplete message from caller to recipient on the AKE topic.
func AkeCompleteSendToRecipient(caller *CallState) ([]byte, error) {
	if caller == nil {
		return nil, errors.New("caller CallState cannot be nil")
	}

	msg, err := CreateAkeCompleteMessage(caller.SenderId, caller.AkeTopic)
	if err != nil {
		return nil, err
	}
	return encryption.SymEncrypt(caller.SharedKey, msg)
}

func ComputeSharedKey(k, tpc, pieA, pieB, A, B, c0, c1, sec []byte) []byte {
	keybytes := helpers.ConcatBytes(k, tpc, pieA, pieB, A, B, c0, c1, sec)
	return helpers.Hash256(keybytes)
}

func CreateZKProof(prover *CallState, chal []byte) ([]byte, error) {
	var telephoneNumber string
	if prover.IamCaller() {
		telephoneNumber = prover.CallerId
	} else {
		telephoneNumber = prover.Recipient
	}

	proof, err := bbs.ZkCreateProof(bbs.AkeZkProof{
		Tn:          telephoneNumber,
		PublicKey:   prover.Config.RtuPublicKey,
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
