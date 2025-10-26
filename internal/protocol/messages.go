package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/dense-identity/denseid/internal/helpers"
)

const (
	TypeAkeInit     = "AkeInit"
	TypeAkeResponse = "AkeResponse"
	TypeAkeComplete = "AkeComplete"
	TypeRuaInit     = "RuaInit"
	TypeBye         = "Bye"
)

type ProtocolMessage struct {
	Type     string          `json:"type"`
	SenderId string          `json:"sender_id"`
	Topic    string          `json:"topic"`
	Payload  json.RawMessage `json:"payload"`
}

func (m *ProtocolMessage) SetPayload(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.Payload = b
	return nil
}

func (m *ProtocolMessage) DecodePayload(out any) error {
	// out should be a pointer to the concrete payload type
	if out == nil {
		return fmt.Errorf("DecodePayload: nil out")
	}
	return json.Unmarshal(m.Payload, out)
}

func (m *ProtocolMessage) Marshal() ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("nil ProtocolMessage")
	}
	if m.Type == "" {
		return nil, fmt.Errorf("missing type")
	}
	return json.Marshal(m)
}

// Unmarshal fills the envelope (type/sender/payload raw bytes).
func (m *ProtocolMessage) Unmarshal(data []byte) error {
	if m == nil {
		return fmt.Errorf("nil ProtocolMessage")
	}
	return json.Unmarshal(data, m)
}

// Convenience: Unmarshal envelope then decode payload into `out`.
func (m *ProtocolMessage) UnmarshalInto(data []byte, out any) error {
	if err := m.Unmarshal(data); err != nil {
		return err
	}
	// if m.Type != TypeAke { return fmt.Errorf("unexpected type %q", m.Type) }
	return m.DecodePayload(out)
}

func (m *ProtocolMessage) IsAkeInit() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeAkeInit
}

func (m *ProtocolMessage) IsAkeResponse() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeAkeResponse
}

func (m *ProtocolMessage) IsAkeComplete() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeAkeComplete
}

func (m *ProtocolMessage) IsRuaInit() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeRuaInit
}

func (m *ProtocolMessage) IsBye() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeBye
}

type AkeMessage struct {
	DhPk       string `json:"dhPk"`
	PublicKey  string `json:"pk"`
	Expiration string `json:"exp"`
	Proof      string `json:"proof"`
}

func (m *AkeMessage) GetDhPk() []byte {
	if m == nil {
		return nil
	}

	data, err := helpers.DecodeHex(m.DhPk)
	if err != nil {
		return nil
	}
	return data
}

func (m *AkeMessage) GetPublicKey() []byte {
	if m == nil {
		return nil
	}

	data, err := helpers.DecodeHex(m.PublicKey)
	if err != nil {
		return nil
	}
	return data
}

func (m *AkeMessage) GetExpiration() []byte {
	if m == nil {
		return nil
	}

	data, err := helpers.DecodeHex(m.Expiration)
	if err != nil {
		return nil
	}
	return data
}

func (m *AkeMessage) GetProof() []byte {
	if m == nil {
		return nil
	}

	data, err := helpers.DecodeHex(m.Proof)
	if err != nil {
		return nil
	}
	return data
}

// Optional helper to parse an AkeInit message envelope directly.
func ParseAkeInitMessage(data []byte) (*AkeMessage, error) {
	var env ProtocolMessage
	if err := env.Unmarshal(data); err != nil {
		return nil, err
	}
	if env.Type != TypeAkeInit {
		return nil, fmt.Errorf("unexpected type %q", env.Type)
	}
	var msg AkeMessage
	if err := env.DecodePayload(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// Optional helper to parse an AkeResponse message envelope directly.
func ParseAkeResponseMessage(data []byte) (*AkeMessage, error) {
	var env ProtocolMessage
	if err := env.Unmarshal(data); err != nil {
		return nil, err
	}
	if env.Type != TypeAkeResponse {
		return nil, fmt.Errorf("unexpected type %q", env.Type)
	}
	var msg AkeMessage
	if err := env.DecodePayload(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// CreateAkeInitMessage creates an AkeInit message (caller -> recipient)
func CreateAkeInitMessage(senderId, topic string, payload *AkeMessage) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     TypeAkeInit,
		SenderId: senderId,
		Topic:    topic,
	}

	if err := msg.SetPayload(payload); err != nil {
		return nil, err
	}

	return msg.Marshal()
}

// CreateAkeResponseMessage creates an AkeResponse message (recipient -> caller)
func CreateAkeResponseMessage(senderId, topic string, payload *AkeMessage) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     TypeAkeResponse,
		SenderId: senderId,
		Topic:    topic,
	}

	if err := msg.SetPayload(payload); err != nil {
		return nil, err
	}

	return msg.Marshal()
}

// CreateRuaInitMessage creates an RuaInit message (caller -> recipient)
func CreateRuaInitMessage(senderId, topic string, payload *AkeMessage) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     TypeRuaInit,
		SenderId: senderId,
		Topic:    topic,
	}

	if err := msg.SetPayload(payload); err != nil {
		return nil, err
	}

	return msg.Marshal()
}

// CreateAkeCompleteMessage creates an AkeComplete message (caller -> recipient)
func CreateAkeCompleteMessage(senderId, topic string) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     TypeAkeComplete,
		SenderId: senderId,
		Topic:    topic,
	}

	return msg.Marshal()
}

// CreateByeMessage creates a bye message to signal session termination
func CreateByeMessage(senderId, topic string) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     TypeBye,
		SenderId: senderId,
		Topic:    topic,
	}

	return msg.Marshal()
}
