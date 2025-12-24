package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/dense-identity/denseid/internal/helpers"
)

const (
	TypeAkeRequest  = "AkeRequest"
	TypeAkeResponse = "AkeResponse"
	TypeAkeComplete = "AkeComplete"

	TypeRuaRequest  = "RuaInit"
	TypeRuaResponse = "RuaResponse"
	TypeHeartBeat   = "HeartBeat"
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

func (m *ProtocolMessage) IsAkeRequest() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeAkeRequest
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
	return m.Type == TypeRuaRequest
}

func (m *ProtocolMessage) IsRuaResponse() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeRuaResponse
}

func (m *ProtocolMessage) IsHeartBeat() bool {
	if m == nil {
		return false
	}
	return m.Type == TypeHeartBeat
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

// CreateAkeMessage creates an AkeInit message (caller -> recipient)
func CreateAkeMessage(senderId, topic, akeType string, payload *AkeMessage) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     akeType,
		SenderId: senderId,
		Topic:    topic,
	}

	if payload != nil {
		if err := msg.SetPayload(payload); err != nil {
			return nil, err
		}
	}

	return msg.Marshal()
}

type RuaMessage struct {
	DhPk   string `json:"dhPk"`
	Reason string `json:"reason"`
	Rtu    Rtu    `json:"rtu"`
}

// CreateRuaMessage creates an RuaInit message (caller -> recipient)
func CreateRuaMessage(senderId, topic, ruaType string, payload *RuaMessage) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     ruaType,
		SenderId: senderId,
		Topic:    topic,
	}

	if err := msg.SetPayload(payload); err != nil {
		return nil, err
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

// CreateHeartBeatMessage creates a heartbeat message to signal session liveness
func CreateHeartBeatMessage(senderId, topic string) ([]byte, error) {
	msg := ProtocolMessage{
		Type:     TypeHeartBeat,
		SenderId: senderId,
		Topic:    topic,
	}

	return msg.Marshal()
}
