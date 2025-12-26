package protocol

import (
	"fmt"

	pb "github.com/dense-identity/denseid/api/go/protocol/v1"
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

// DecodeAkePayload extracts AkeMessage from ProtocolMessage payload
func DecodeAkePayload(m *ProtocolMessage) (*AkeMessage, error) {
	if m == nil {
		return nil, fmt.Errorf("nil ProtocolMessage")
	}
	ake := &AkeMessage{}
	if err := proto.Unmarshal(m.Payload, ake); err != nil {
		return nil, fmt.Errorf("failed to decode AKE payload: %v", err)
	}
	return ake, nil
}

// DecodeRuaPayload extracts RuaMessage from ProtocolMessage payload
func DecodeRuaPayload(m *ProtocolMessage) (*RuaMessage, error) {
	if m == nil {
		return nil, fmt.Errorf("nil ProtocolMessage")
	}
	rua := &RuaMessage{}
	if err := proto.Unmarshal(m.Payload, rua); err != nil {
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

// CreateAkeMessage creates an AKE protocol message
func CreateAkeMessage(senderId, topic string, msgType MessageType, payload *AkeMessage) ([]byte, error) {
	var payloadBytes []byte
	var err error

	if payload != nil {
		payloadBytes, err = proto.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal AKE payload: %v", err)
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

// CreateRuaMessage creates an RUA protocol message
func CreateRuaMessage(senderId, topic string, msgType MessageType, payload *RuaMessage) ([]byte, error) {
	var payloadBytes []byte
	var err error

	if payload != nil {
		payloadBytes, err = proto.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal RUA payload: %v", err)
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