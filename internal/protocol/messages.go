package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/dense-identity/denseid/internal/helpers"
)

const (
	TypeAke = "Ake"
	TypeBye = "Bye"

	AkeRound1 = 1
	AkeRound2 = 2
)

type ProtocolMessage struct {
	Type     string          `json:"type"`
	SenderId string          `json:"sender_id"`
	Round    int             `json:"round"`
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

func (m *ProtocolMessage) IsAke() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeAke
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

func (m *ProtocolMessage) IsRoundOne() bool {
	return m.Round == AkeRound1
}

func (m *ProtocolMessage) IsRoundTwo() bool {
	return m.Round == AkeRound2
}

// Optional helper to parse an Ake envelope directly.
func ParseAkeMessage(data []byte) (*AkeMessage, error) {
	var env ProtocolMessage
	if err := env.Unmarshal(data); err != nil {
		return nil, err
	}
	if env.Type != TypeAke {
		return nil, fmt.Errorf("unexpected type %q", env.Type)
	}
	var msg AkeMessage
	if err := env.DecodePayload(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// CreateByeMessage creates a bye message to signal completion
func CreateByeMessage(senderId string) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     TypeBye,
		SenderId: senderId,
		Round:    0, // Bye messages don't need rounds
	}

	return msg.Marshal()
}
