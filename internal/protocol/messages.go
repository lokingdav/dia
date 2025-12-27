package protocol

import (
	"fmt"

	pb "github.com/dense-identity/denseid/api/go/protocol/v1"
	"github.com/dense-identity/denseid/internal/encryption"
	"google.golang.org/protobuf/proto"
)

// Type aliases for convenience
type MessageType = pb.MessageType
type ProtocolMessage = pb.ProtocolMessage
type AkeMessage = pb.AkeMessage
type RuaMessage = pb.RuaMessage
type Rtu = pb.Rtu

// Message type constants for convenience
const (
	TypeAkeRequest  = pb.MessageType_AKE_REQUEST
	TypeAkeResponse = pb.MessageType_AKE_RESPONSE
	TypeAkeComplete = pb.MessageType_AKE_COMPLETE
	TypeRuaRequest  = pb.MessageType_RUA_REQUEST
	TypeRuaResponse = pb.MessageType_RUA_RESPONSE
	TypeHeartBeat   = pb.MessageType_HEARTBEAT
	TypeBye         = pb.MessageType_BYE
)

// MarshalMessage serializes a ProtocolMessage to bytes
func MarshalMessage(m *ProtocolMessage) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("nil ProtocolMessage")
	}
	if m.Type == pb.MessageType_MESSAGE_TYPE_UNSPECIFIED {
		return nil, fmt.Errorf("missing type")
	}
	return proto.Marshal(m)
}

// UnmarshalMessage deserializes bytes into a ProtocolMessage
func UnmarshalMessage(data []byte) (*ProtocolMessage, error) {
	m := &ProtocolMessage{}
	if err := proto.Unmarshal(data, m); err != nil {
		return nil, err
	}
	return m, nil
}

// DecodeAkePayload extracts AkeMessage from ProtocolMessage payload.
// If pkePrivateKey is provided, the payload is decrypted first using PKE.
func DecodeAkePayload(m *ProtocolMessage, pkePrivateKey []byte) (*AkeMessage, error) {
	if m == nil {
		return nil, fmt.Errorf("nil ProtocolMessage")
	}

	payloadBytes := m.Payload

	// If PKE key is provided, decrypt the payload first
	if len(pkePrivateKey) > 0 {
		decrypted, err := encryption.PkeDecrypt(pkePrivateKey, payloadBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt AKE payload: %v", err)
		}
		payloadBytes = decrypted
	}

	ake := &AkeMessage{}
	if err := proto.Unmarshal(payloadBytes, ake); err != nil {
		return nil, fmt.Errorf("failed to decode AKE payload: %v", err)
	}
	return ake, nil
}

// DecodeRuaPayload extracts RuaMessage from ProtocolMessage payload.
// If sharedKey is provided, the payload is decrypted first using symmetric encryption.
func DecodeRuaPayload(m *ProtocolMessage, sharedKey []byte) (*RuaMessage, error) {
	if m == nil {
		return nil, fmt.Errorf("nil ProtocolMessage")
	}

	payloadBytes := m.Payload

	// If shared key is provided, decrypt the payload first
	if len(sharedKey) > 0 {
		decrypted, err := encryption.SymDecrypt(sharedKey, payloadBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt RUA payload: %v", err)
		}
		payloadBytes = decrypted
	}

	rua := &RuaMessage{}
	if err := proto.Unmarshal(payloadBytes, rua); err != nil {
		return nil, fmt.Errorf("failed to decode RUA payload: %v", err)
	}
	return rua, nil
}

// IsAkeRequest checks if the message is an AKE request
func IsAkeRequest(m *ProtocolMessage) bool {
	return m != nil && m.Type == TypeAkeRequest
}

// IsAkeResponse checks if the message is an AKE response
func IsAkeResponse(m *ProtocolMessage) bool {
	return m != nil && m.Type == TypeAkeResponse
}

// IsAkeComplete checks if the message is an AKE complete
func IsAkeComplete(m *ProtocolMessage) bool {
	return m != nil && m.Type == TypeAkeComplete
}

// IsRuaRequest checks if the message is an RUA request
func IsRuaRequest(m *ProtocolMessage) bool {
	return m != nil && m.Type == TypeRuaRequest
}

// IsRuaResponse checks if the message is an RUA response
func IsRuaResponse(m *ProtocolMessage) bool {
	return m != nil && m.Type == TypeRuaResponse
}

// IsHeartBeat checks if the message is a heartbeat
func IsHeartBeat(m *ProtocolMessage) bool {
	return m != nil && m.Type == TypeHeartBeat
}

// IsBye checks if the message is a bye
func IsBye(m *ProtocolMessage) bool {
	return m != nil && m.Type == TypeBye
}

// CreateAkeMessage creates an AKE protocol message.
// If recipientPkePk is provided, the payload is encrypted using PKE.
func CreateAkeMessage(senderId, topic string, msgType MessageType, payload *AkeMessage, recipientPkePk []byte) ([]byte, error) {
	var payloadBytes []byte
	var err error

	if payload != nil {
		payloadBytes, err = proto.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal AKE payload: %v", err)
		}

		// Encrypt payload if recipient's PKE public key is provided
		if len(recipientPkePk) > 0 {
			payloadBytes, err = encryption.PkeEncrypt(recipientPkePk, payloadBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt AKE payload: %v", err)
			}
		}
	}

	msg := &ProtocolMessage{
		Type:     msgType,
		SenderId: senderId,
		Topic:    topic,
		Payload:  payloadBytes,
	}

	return MarshalMessage(msg)
}

// CreateRuaMessage creates an RUA protocol message.
// If sharedKey is provided, the payload is encrypted using symmetric encryption.
// DEPRECATED: Use CreateDrRuaMessage for Double Ratchet encryption.
func CreateRuaMessage(senderId, topic string, msgType MessageType, payload *RuaMessage, sharedKey []byte) ([]byte, error) {
	var payloadBytes []byte
	var err error

	if payload != nil {
		payloadBytes, err = proto.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal RUA payload: %v", err)
		}

		// Encrypt payload if shared key is provided
		if len(sharedKey) > 0 {
			payloadBytes, err = encryption.SymEncrypt(sharedKey, payloadBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt RUA payload: %v", err)
			}
		}
	}

	msg := &ProtocolMessage{
		Type:     msgType,
		SenderId: senderId,
		Topic:    topic,
		Payload:  payloadBytes,
	}

	return MarshalMessage(msg)
}

// DrMessage type alias for Double Ratchet messages
type DrMessage = pb.DrMessage
type DrHeader = pb.DrHeader

// CreateDrRuaMessage creates an RUA protocol message encrypted with Double Ratchet.
// The payload is encrypted using the DR session and wrapped in a DrMessage.
func CreateDrRuaMessage(senderId, topic string, msgType MessageType, payload *RuaMessage, drSession *DrSession) ([]byte, error) {
	if drSession == nil {
		return nil, fmt.Errorf("DR session not initialized")
	}

	var payloadBytes []byte
	var err error

	if payload != nil {
		payloadBytes, err = proto.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal RUA payload: %v", err)
		}

		// Encrypt using Double Ratchet
		drMsg, err := DrEncrypt(drSession, payloadBytes, []byte(topic))
		if err != nil {
			return nil, fmt.Errorf("failed to DR encrypt RUA payload: %v", err)
		}

		// Serialize the DrMessage as the payload
		payloadBytes, err = proto.Marshal(drMsg)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal DR message: %v", err)
		}
	}

	msg := &ProtocolMessage{
		Type:     msgType,
		SenderId: senderId,
		Topic:    topic,
		Payload:  payloadBytes,
	}

	return MarshalMessage(msg)
}

// DecodeDrRuaPayload extracts RuaMessage from ProtocolMessage payload using Double Ratchet decryption.
func DecodeDrRuaPayload(m *ProtocolMessage, drSession *DrSession) (*RuaMessage, error) {
	if m == nil {
		return nil, fmt.Errorf("nil ProtocolMessage")
	}
	if drSession == nil {
		return nil, fmt.Errorf("DR session not initialized")
	}

	// First unmarshal the DrMessage
	drMsg := &DrMessage{}
	if err := proto.Unmarshal(m.Payload, drMsg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DR message: %v", err)
	}

	// Decrypt using Double Ratchet
	plaintext, err := DrDecrypt(drSession, drMsg, []byte(m.Topic))
	if err != nil {
		return nil, fmt.Errorf("failed to DR decrypt RUA payload: %v", err)
	}

	// Unmarshal the decrypted RuaMessage
	rua := &RuaMessage{}
	if err := proto.Unmarshal(plaintext, rua); err != nil {
		return nil, fmt.Errorf("failed to decode RUA payload: %v", err)
	}
	return rua, nil
}

// CreateByeMessage creates a bye message to signal session termination
func CreateByeMessage(senderId, topic string) ([]byte, error) {
	msg := &ProtocolMessage{
		Type:     TypeBye,
		SenderId: senderId,
		Topic:    topic,
	}
	return MarshalMessage(msg)
}

// CreateHeartBeatMessage creates a heartbeat message to signal session liveness
func CreateHeartBeatMessage(senderId, topic string) ([]byte, error) {
	msg := &ProtocolMessage{
		Type:     TypeHeartBeat,
		SenderId: senderId,
		Topic:    topic,
	}
	return MarshalMessage(msg)
}

// MarshalDDA returns the deterministic data for AMF signature verification (Data to be signed/verified).
// It marshals the RuaMessage without the sigma field.
func MarshalDDA(m *RuaMessage) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("nil RuaMessage")
	}

	ruaMsg := &RuaMessage{
		DhPk:   m.DhPk,
		Tpc:    m.Tpc,
		Reason: m.Reason,
		Rtu:    m.Rtu,
	}

	return proto.MarshalOptions{Deterministic: true}.Marshal(ruaMsg)
}
