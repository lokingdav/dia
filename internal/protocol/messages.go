package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	TypeAkeM1 = "AkeM1"
	TypeAkeM2 = "AkeM2"
)

type MessageHeader struct {
	Type     string `json:"type"`
	SenderId string `json:"sender_id"`
}

type AkeMessage1Body struct {
	DhPk  string `json:"dhPk"`
	Proof string `json:"proof"`
}

type AkeMessage1 struct {
	Header MessageHeader   `json:"header"`
	Body   AkeMessage1Body `json:"body"`
}

func (m *AkeMessage1) Marshal() ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("nil AkeMessage1")
	}

	if m.Header.Type == "" {
		m.Header.Type = TypeAkeM1
	} else if m.Header.Type != TypeAkeM1 {
		return nil, fmt.Errorf("invalid header.Type %q (want %s)", m.Header.Type, TypeAkeM1)
	}

	if m.Header.SenderId == "" {
		return nil, fmt.Errorf("missing header.SenderId")
	}

	if m.Body.DhPk == "" || m.Body.Proof == "" {
		return nil, fmt.Errorf("missing dhpk or proof fields. Both are required")
	}

	return json.Marshal(m)
}

func (m *AkeMessage1) Unmarshal(data []byte) error {
	if m == nil {
		return errors.New("nil AkeMessage1 receiver")
	}

	// Decode into a temp to avoid partial state on error.
	var tmp AkeMessage1
	if err := json.Unmarshal(data, &tmp); err != nil {
		return fmt.Errorf("decode AkeMessage1: %w", err)
	}

	// Validate header.type matches this message kind.
	if tmp.Header.Type != TypeAkeM1 {
		return fmt.Errorf("unexpected header.type %q (want %s)", tmp.Header.Type, TypeAkeM1)
	}

	if tmp.Header.SenderId == "" {
		return errors.New("missing header.sender_id")
	}
	if tmp.Body.DhPk == "" {
		return errors.New("missing body.dhPk")
	}
	if tmp.Body.Proof == "" {
		return errors.New("missing body.proof")
	}

	*m = tmp
	return nil
}
