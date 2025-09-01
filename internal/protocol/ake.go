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

func AkeM1CallerToRecipient(callState *CallState) ([]byte, error) {
	c0 := AkeChallenge0(callState.SharedKey, callState.DhPk, callState.CallerId, callState.Ts)
	proof, err := bbs.ZkProof(c0)
	if err != nil {
		return nil, err
	}

	akeM1 := AkeMessage1{
		Header: MessageHeader{
			SenderId: callState.SenderId,
		},
		Body: AkeMessage1Body{
			DhPk:  helpers.EncodeToHex(callState.DhPk),
			Proof: helpers.EncodeToHex(proof),
		},
	}

	msg, err := akeM1.Marshal()

	if err != nil {
		return nil, err
	}

	return encryption.SymEncrypt(callState.SharedKey, msg)
}
