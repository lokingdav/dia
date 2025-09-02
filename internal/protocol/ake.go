package protocol

import (
	"context"
	"errors"
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

	// Deadline for Evaluate
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

func InitAke(ctx context.Context, callState *CallState) error {
	if callState == nil {
		return errors.New("nil CallState")
	}

	topic := helpers.Hash256Hex(helpers.ConcatBytes(callState.SharedKey, []byte("1")))

	dhSk, dhPk, err := dia.DHKeygen()
	if err != nil {
		return err
	}

	callState.InitAke(dhSk, dhPk, topic)

	return nil
}

func AkeRound1CallerToRecipient(caller *CallState) ([]byte, error) {
	if caller == nil {
		return nil, errors.New("caller CallState cannot be nil")
	}

	c0 := helpers.Hash256(helpers.ConcatBytes(caller.SharedKey, caller.DhPk, caller.GetAkeLabel()))
	proof, err := bbs.ZkProof(c0)
	if err != nil {
		return nil, err
	}

	akeMsg := AkeMessage{
		Round:    AkeRound1,
		SenderId: caller.SenderId,
		DhPk:     helpers.EncodeToHex(caller.DhPk),
		Proof:    helpers.EncodeToHex(proof),
	}

	msg, err := akeMsg.Marshal()

	if err != nil {
		return nil, err
	}

	caller.UpdateR1(c0, proof)

	return encryption.SymEncrypt(caller.SharedKey, msg)
}

func AkeRound2RecipientToCaller(recipient *CallState, caller *AkeMessage) ([]byte, error) {
	if recipient == nil {
		return nil, errors.New("recipient CallState cannot be nil")
	}
	if caller == nil {
		return nil, errors.New("caller AkeMessage cannot be nil")
	}
	if !caller.IsRoundOne() {
		return nil, errors.New("AkeRound2RecipientToCaller can only be called on Round1 message")
	}

	callerDhPk := caller.GetDhPk()
	callerProof := caller.GetProof()

	c0 := helpers.Hash256(helpers.ConcatBytes(recipient.SharedKey, callerDhPk, recipient.GetAkeLabel()))
	if !bbs.ZkVerify(callerProof, c0, recipient.CallerId) {
		return nil, errors.New("unauthenticated")
	}

	c1 := helpers.Hash256(helpers.ConcatBytes(callerProof, callerDhPk, recipient.DhPk, c0))
	proof, err := bbs.ZkProof(c1)
	if err != nil {
		return nil, err
	}

	akeMsg := AkeMessage{
		Round:    AkeRound2,
		SenderId: recipient.SenderId,
		DhPk:     helpers.EncodeToHex(recipient.DhPk),
		Proof:    helpers.EncodeToHex(proof),
	}

	msg, err := akeMsg.Marshal()
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
		recipient.MarshalTopic(),
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

func AkeRound2CallerFinalize(caller *CallState, recipient *AkeMessage) error {
	if caller == nil {
		return errors.New("caller CallState cannot be nil")
	}
	if recipient == nil {
		return errors.New("recipient AkeMessage cannot be nil")
	}
	if !recipient.IsRoundTwo() {
		return errors.New("AkeRound2CallerFinalize can only be called on Round2 message")
	}

	recipientDhPk, err1 := helpers.DecodeHex(recipient.DhPk)
	recipientProof, err2 := helpers.DecodeHex(recipient.Proof)
	if err1 != nil || err2 != nil {
		return errors.New("something unexpected happened")
	}

	c1 := helpers.Hash256(helpers.ConcatBytes(caller.Proof, caller.DhPk, recipientDhPk, caller.Chal0))
	if !bbs.ZkVerify(recipientProof, c1, caller.Recipient) {
		return errors.New("unauthenticated")
	}

	secret, err := dia.DHComputeSecret(caller.DhSk, recipientDhPk)
	if err != nil {
		return err
	}

	caller.SetSharedKey(ComputeSharedKey(
		caller.SharedKey,
		caller.MarshalTopic(),
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

func ComputeSharedKey(k, tpc, pieA, pieB, A, B, c0, c1, sec []byte) []byte {
	// fmt.Printf("\nk:\t%x\n", k)
	// fmt.Printf("tpc:\t%x\n", tpc)
	// fmt.Printf("pieA:\t%x\n", pieA)
	// fmt.Printf("pieB:\t%x\n", pieB)
	// fmt.Printf("A:\t%x\n", A)
	// fmt.Printf("B:\t%x\n", B)
	// fmt.Printf("c0:\t%x\n", c0)
	// fmt.Printf("c1:\t%x\n", c1)
	// fmt.Printf("sec:\t%x\n\n", sec)
	keybytes := helpers.ConcatBytes(k, tpc, pieA, pieB, A, B, c0, c1, sec)
	return helpers.Hash256(keybytes)
}
