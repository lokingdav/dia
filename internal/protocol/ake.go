package protocol

import (
	"context"
	"errors"
	"time"

	keypb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	"github.com/dense-identity/denseid/internal/bbs"
	"github.com/dense-identity/denseid/internal/helpers"
	"github.com/dense-identity/denseid/internal/voprf"
	dia "github.com/lokingdav/libdia/bindings/go"
)

type InitAkeResponse struct {
	DhSk, DhPk, SharedKey []byte
	Topic string
}

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

	blinded, blind, err := voprf.Blind(callState.CallDetail.GetAkeLabel())
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

func InitAke(ctx context.Context, client keypb.KeyDerivationServiceClient, callState *CallState) (InitAkeResponse, error) {
	if callState == nil {
		return InitAkeResponse{}, errors.New("nil CallState")
	}

	dhSk, dhPk, err := dia.DHKeygen()
	if err != nil {
		return InitAkeResponse{}, err
	}

	topic := helpers.Hash256Hex(helpers.ConcatBytes(callState.SharedKey, []byte("1")))
	callState.SetTopic(topic)
	return InitAkeResponse{
		DhSk: dhSk,
		DhPk: dhPk,
		SharedKey: callState.SharedKey,
		Topic: topic,
	}, nil
}

func VerifyAkeZK(proof []byte, chal []byte, tn string) {}

func CreateAkeState(callerPk, recipientPk, sharedKey []byte) {}

func M1CallerToRecipient(
	callDetail CallDetail, 
	initAkeRes InitAkeResponse) (topic string, ciphertext []byte, err error) {
	c0 := AkeChallenge0(initAkeRes.SharedKey, initAkeRes.DhPk, callDetail.CallerId, callDetail.Ts)
	proof, err := bbs.ZkProof(c0)
	if err != nil {
		return
	}

	message := AkeMessage1{
		DhPk: initAkeRes.DhPk,
		ZkProof: proof,
	}

	ciphertext, err = message.Encrypt(initAkeRes.SharedKey)
	if err != nil {
		return
	}

	topic = initAkeRes.Topic

	return
}

// func M2RecipientToCaller(
// 	callDetail CallDetail, 
// 	initAkeRes InitAkeResponse, 
// 	m1Ciphertext []byte) (tpc string, ciphertext []byte, err error) {
// 	c0 := AkeChallenge0(initAkeRes.SharedKey, message1.DhPk, callDetail.CallerId, callDetail.Ts)
// 	proof, err := bbs.ZkProof(c0)

// 	if err != nil {
// 		return
// 	}

// 	message := AkeMessage1{
// 		DhPk: initAkeRes.DhPk,
// 		ZkProof: proof,
// 	}

// 	ciphertext, err = message.Encrypt(initAkeRes.SharedKey)
// 	if err != nil {
// 		return
// 	}

// 	tpc = initAkeRes.Tpc

// 	return
// }