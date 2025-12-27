package protocol

import (
	"errors"

	"github.com/dense-identity/denseid/internal/amf"
	"github.com/dense-identity/denseid/internal/bbs"
	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
	dia "github.com/lokingdav/libdia/bindings/go"
	"google.golang.org/protobuf/proto"
)

// createRtuFromConfig builds an RTU from enrollment data in SubscriberConfig
func createRtuFromConfig(cfg *config.SubscriberConfig) *Rtu {
	return &Rtu{
		AmfPk:      cfg.AmfPublicKey,
		PkePk:      cfg.PkePublicKey,
		Expiration: cfg.EnExpiration,
		Signature:  cfg.RaSignature,
		Name:       cfg.MyName,
	}
}

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

func InitRTU(party *CallState) error {
	ruaTopic := DeriveRuaTopic(party)
	rtu := createRtuFromConfig(party.Config)

	dhSk, dhPk, err := dia.DHKeygen()
	if err != nil {
		return err
	}

	party.Rua = RuaState{
		Topic: ruaTopic,
		DhSk:  dhSk,
		DhPk:  dhPk,
		Rtu:   rtu,
	}

	return nil
}

func VerifyRTU(party *CallState, tn string, msg *RuaMessage) error {
	if msg == nil {
		return errors.New("RuaMessage cannot be nil")
	}
	if msg.Rtu == nil {
		return errors.New("RTU cannot be nil")
	}

	// Validate if exp in rua message has expired
	if err := datetime.CheckExpiry(msg.Rtu.Expiration); err != nil {
		return err
	}

	// Validate RA's BBS signature on RTU
	message1 := helpers.HashAll(msg.Rtu.AmfPk, msg.Rtu.PkePk, msg.Rtu.Expiration, []byte(tn))
	message2 := []byte(msg.Rtu.Name)
	messages := [][]byte{message1, message2}
	rtuValid, err := bbs.Verify(messages, party.Config.RaPublicKey, msg.Rtu.Signature)
	if err != nil {
		return err
	}
	if !rtuValid {
		return errors.New("invalid RTU signature from RA")
	}

	// Validate AMF signature (sigma) in rua message
	data, err := MarshalDDA(msg)
	if err != nil {
		return err
	}

	sigmaValid, err := amf.Verify(
		msg.Rtu.AmfPk,
		party.Config.AmfPrivateKey,
		party.Config.ModeratorPublicKey,
		data,
		msg.Sigma)

	if err != nil {
		return err
	}
	if !sigmaValid {
		return errors.New("invalid AMF signature")
	}

	return nil
}

// RuaRequest creates RuaRequest message
func RuaRequest(caller *CallState) ([]byte, error) {
	if caller == nil {
		return nil, errors.New("caller CallState cannot be nil")
	}

	// Use existing RTU from state, or initialize if not set
	if caller.Rua.Topic == nil {
		InitRTU(caller)
	}

	topic := helpers.EncodeToHex(caller.Rua.Topic)

	ruaMsg := &RuaMessage{
		DhPk:   caller.Rua.DhPk,
		Tpc:    topic,
		Reason: caller.CallReason,
		Rtu:    caller.Rua.Rtu,
	}

	data, err := MarshalDDA(ruaMsg)
	if err != nil {
		return nil, err
	}

	Sigma, err := amf.Sign(
		caller.Config.AmfPrivateKey,
		caller.CounterpartAmfPk,
		caller.Config.ModeratorPublicKey,
		data)
	if err != nil {
		return nil, err
	}

	ruaMsg.Sigma = Sigma
	caller.Rua.Req = ruaMsg

	msg, err := CreateRuaMessage(caller.SenderId, topic, TypeRuaRequest, ruaMsg)
	if err != nil {
		return nil, err
	}

	ciphertext, err := encryption.SymEncrypt(caller.SharedKey, msg)
	if err != nil {
		return nil, err
	}

	return ciphertext, nil
}

func RuaResponse(recipient *CallState, callerMsg *ProtocolMessage) ([]byte, error) {
	if recipient == nil {
		return nil, errors.New("recipient CallState cannot be nil")
	}
	if callerMsg == nil {
		return nil, errors.New("caller ProtocolMessage cannot be nil")
	}
	if !IsRuaRequest(callerMsg) {
		return nil, errors.New("AkeResponse can only be called on AkeRequest message")
	}

	// Decode the message from the protocol message
	caller, err := DecodeRuaPayload(callerMsg)
	if err != nil {
		return nil, err
	}

	err = VerifyRTU(recipient, recipient.Src, caller)
	if err != nil {
		return nil, err
	}

	ddA, err := MarshalDDA(caller)
	if err != nil {
		return nil, err
	}

	reply := &RuaMessage{
		DhPk: recipient.Rua.DhPk,
		Rtu:  recipient.Rua.Rtu,
		Misc: ddA,
	}

	ddB, err := proto.MarshalOptions{Deterministic: true}.Marshal(reply)
	if err != nil {
		return nil, err
	}

	Sigma, err := amf.Sign(
		recipient.Config.AmfPrivateKey,
		recipient.CounterpartAmfPk,
		recipient.Config.ModeratorPublicKey,
		ddB)
	if err != nil {
		return nil, err
	}

	reply.Sigma = Sigma
	reply.Misc = nil
	tpc := helpers.EncodeToHex(recipient.Rua.Topic)
	msg, err := CreateRuaMessage(recipient.SenderId, tpc, TypeRuaResponse, reply)
	if err != nil {
		return nil, err
	}

	ciphertext, err := encryption.SymEncrypt(recipient.SharedKey, msg)
	if err != nil {
		return nil, err
	}

	secret, err := dia.DHComputeSecret(recipient.Rua.DhSk, caller.DhPk)
	if err != nil {
		return nil, err
	}

	// ctxA, err :=
	rtuB, err := proto.MarshalOptions{Deterministic: true}.Marshal(caller.Rtu)
	sharedKey := helpers.HashAll(
		ddA,
		reply.DhPk,
		rtuB,
		caller.Sigma,
		Sigma,
		secret,
	)
	recipient.SetSharedKey(sharedKey)

	return ciphertext, nil
}

func RuaFinalize(caller *CallState, recipientMsg *ProtocolMessage) error {
	if caller == nil {
		return errors.New("caller CallState cannot be nil")
	}
	if recipientMsg == nil {
		return errors.New("recipient ProtocolMessage cannot be nil")
	}
	if !IsRuaResponse(recipientMsg) {
		return errors.New("RuaFinalize can only be called on RuaResponse")
	}

	recipient, err := DecodeRuaPayload(recipientMsg)
	if err != nil {
		return err
	}

	// Verify RTU validity (expiration and BBS signature) but not AMF signature
	// because RuaResponse has a different signing structure
	if recipient.Rtu == nil {
		return errors.New("RTU cannot be nil")
	}
	if err := datetime.CheckExpiry(recipient.Rtu.Expiration); err != nil {
		return err
	}

	// Validate RA's BBS signature on RTU
	message1 := helpers.HashAll(recipient.Rtu.AmfPk, recipient.Rtu.PkePk, recipient.Rtu.Expiration, []byte(caller.Dst))
	message2 := []byte(recipient.Rtu.Name)
	messages := [][]byte{message1, message2}
	rtuValid, err := bbs.Verify(messages, caller.Config.RaPublicKey, recipient.Rtu.Signature)
	if err != nil {
		return err
	}
	if !rtuValid {
		return errors.New("invalid RTU signature from RA")
	}

	// Verify AMF signature on RuaResponse
	// RuaResponse was signed with {DhPk, Rtu, Misc=ddA}
	ddA, err := MarshalDDA(caller.Rua.Req)
	if err != nil {
		return err
	}

	// Reconstruct what was signed
	signedMsg := &RuaMessage{
		DhPk: recipient.DhPk,
		Rtu:  recipient.Rtu,
		Misc: ddA,
	}
	signedData, err := proto.MarshalOptions{Deterministic: true}.Marshal(signedMsg)
	if err != nil {
		return err
	}

	sigmaValid, err := amf.Verify(
		recipient.Rtu.AmfPk,
		caller.Config.AmfPrivateKey,
		caller.Config.ModeratorPublicKey,
		signedData,
		recipient.Sigma)
	if err != nil {
		return err
	}
	if !sigmaValid {
		return errors.New("invalid AMF signature")
	}

	secret, err := dia.DHComputeSecret(caller.Rua.DhSk, recipient.DhPk)
	if err != nil {
		return err
	}

	// Use caller's RTU (from the original request) to match RuaResponse's shared key derivation
	rtuB, err := proto.MarshalOptions{Deterministic: true}.Marshal(caller.Rua.Req.Rtu)
	sharedKey := helpers.HashAll(
		ddA,
		recipient.DhPk,
		rtuB,
		caller.Rua.Req.Sigma,
		recipient.Sigma,
		secret)
	caller.SetSharedKey(sharedKey)

	return nil
}
