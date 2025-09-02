package protocol

import (
	"encoding/json"
	"testing"
)

// TestProtocolMessageMarshalUnmarshal tests basic protocol message serialization
func TestProtocolMessageMarshalUnmarshal(t *testing.T) {
	// Create a test protocol message
	original := ProtocolMessage{
		Type:     TypeAke,
		SenderId: "test_sender_123",
	}

	// Set a test payload
	testPayload := map[string]interface{}{
		"test_field": "test_value",
		"number":     42,
	}

	err := original.SetPayload(testPayload)
	if err != nil {
		t.Fatalf("failed to set payload: %v", err)
	}

	// Marshal
	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal
	var restored ProtocolMessage
	err = restored.Unmarshal(data)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify fields
	if restored.Type != original.Type {
		t.Errorf("type mismatch: got %s, want %s", restored.Type, original.Type)
	}

	if restored.SenderId != original.SenderId {
		t.Errorf("senderId mismatch: got %s, want %s", restored.SenderId, original.SenderId)
	}

	// Verify payload can be decoded
	var decodedPayload map[string]interface{}
	err = restored.DecodePayload(&decodedPayload)
	if err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}

	if decodedPayload["test_field"] != "test_value" {
		t.Errorf("payload field mismatch: got %v, want test_value", decodedPayload["test_field"])
	}
}

// TestAkeMessageMarshalUnmarshal tests AKE message serialization
func TestAkeMessageMarshalUnmarshal(t *testing.T) {
	// Create test AKE message
	original := AkeMessage{
		Round:    AkeRound1,
		DhPk:     "abcdef1234567890",
		Proof:    "proof_data_here",
		SenderId: "alice",
	}

	// Marshal
	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal AKE message: %v", err)
	}

	// Parse using the helper function
	restored, err := ParseAkeMessage(data)
	if err != nil {
		t.Fatalf("failed to parse AKE message: %v", err)
	}

	// Verify all fields
	if restored.Round != original.Round {
		t.Errorf("round mismatch: got %d, want %d", restored.Round, original.Round)
	}

	if restored.DhPk != original.DhPk {
		t.Errorf("dhPk mismatch: got %s, want %s", restored.DhPk, original.DhPk)
	}

	if restored.Proof != original.Proof {
		t.Errorf("proof mismatch: got %s, want %s", restored.Proof, original.Proof)
	}

	if restored.SenderId != original.SenderId {
		t.Errorf("senderId mismatch: got %s, want %s", restored.SenderId, original.SenderId)
	}

	// Test round detection
	if !restored.IsRoundOne() {
		t.Error("should be round one")
	}

	if restored.IsRoundTwo() {
		t.Error("should not be round two")
	}
}

// TestAkeMessageValidation tests AKE message validation
func TestAkeMessageValidation(t *testing.T) {
	testCases := []struct {
		name        string
		message     AkeMessage
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_message",
			message: AkeMessage{
				Round:    AkeRound1,
				DhPk:     "valid_dhpk",
				Proof:    "valid_proof",
				SenderId: "valid_sender",
			},
			expectError: false,
		},
		{
			name: "missing_dhpk",
			message: AkeMessage{
				Round:    AkeRound1,
				DhPk:     "", // empty
				Proof:    "valid_proof",
				SenderId: "valid_sender",
			},
			expectError: true,
			errorMsg:    "missing dhPk or proof (both required)",
		},
		{
			name: "missing_proof",
			message: AkeMessage{
				Round:    AkeRound1,
				DhPk:     "valid_dhpk",
				Proof:    "", // empty
				SenderId: "valid_sender",
			},
			expectError: true,
			errorMsg:    "missing dhPk or proof (both required)",
		},
		{
			name: "missing_sender_id",
			message: AkeMessage{
				Round:    AkeRound1,
				DhPk:     "valid_dhpk",
				Proof:    "valid_proof",
				SenderId: "", // empty
			},
			expectError: true,
			errorMsg:    "missing dhPk or proof (both required)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.message.Marshal()

			if tc.expectError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				if tc.errorMsg != "" && err.Error() != tc.errorMsg {
					t.Errorf("error message mismatch: got %q, want %q", err.Error(), tc.errorMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestProtocolMessageValidation tests protocol message validation
func TestProtocolMessageValidation(t *testing.T) {
	t.Run("nil_message", func(t *testing.T) {
		var msg *ProtocolMessage
		_, err := msg.Marshal()
		if err == nil {
			t.Fatal("expected error for nil message")
		}
	})

	t.Run("missing_type", func(t *testing.T) {
		msg := ProtocolMessage{
			SenderId: "test",
		}
		_, err := msg.Marshal()
		if err == nil {
			t.Fatal("expected error for missing type")
		}
	})

	t.Run("nil_payload_decode", func(t *testing.T) {
		msg := ProtocolMessage{
			Type: TypeAke,
		}
		err := msg.DecodePayload(nil)
		if err == nil {
			t.Fatal("expected error for nil payload decode target")
		}
	})
}

// TestAkeRoundHelpers tests round detection helpers
func TestAkeRoundHelpers(t *testing.T) {
	round1Msg := AkeMessage{Round: AkeRound1}
	round2Msg := AkeMessage{Round: AkeRound2}

	// Test Round 1
	if !round1Msg.IsRoundOne() {
		t.Error("Round 1 message should be detected as round one")
	}
	if round1Msg.IsRoundTwo() {
		t.Error("Round 1 message should not be detected as round two")
	}

	// Test Round 2
	if round2Msg.IsRoundOne() {
		t.Error("Round 2 message should not be detected as round one")
	}
	if !round2Msg.IsRoundTwo() {
		t.Error("Round 2 message should be detected as round two")
	}
}

// TestProtocolMessageIsAke tests AKE type detection
func TestProtocolMessageIsAke(t *testing.T) {
	akeMsg := ProtocolMessage{Type: TypeAke}
	nonAkeMsg := ProtocolMessage{Type: "SomeOtherType"}
	var nilMsg *ProtocolMessage

	if !akeMsg.IsAke() {
		t.Error("AKE message should be detected as AKE")
	}

	if nonAkeMsg.IsAke() {
		t.Error("non-AKE message should not be detected as AKE")
	}

	if nilMsg.IsAke() {
		t.Error("nil message should not be detected as AKE")
	}
}

// TestComplexPayloadHandling tests complex payload encoding/decoding
func TestComplexPayloadHandling(t *testing.T) {
	// Create a protocol message with AKE payload
	akePayload := AkeMessage{
		Round:    AkeRound2,
		DhPk:     "complex_dhpk_data",
		Proof:    "complex_proof_data",
		SenderId: "test_sender",
	}

	protocolMsg := ProtocolMessage{
		Type:     TypeAke,
		SenderId: "envelope_sender",
	}

	err := protocolMsg.SetPayload(akePayload)
	if err != nil {
		t.Fatalf("failed to set AKE payload: %v", err)
	}

	// Marshal the envelope
	data, err := protocolMsg.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal protocol message: %v", err)
	}

	// Use UnmarshalInto to parse both envelope and payload
	var restoredProtocol ProtocolMessage
	var restoredAke AkeMessage

	err = restoredProtocol.UnmarshalInto(data, &restoredAke)
	if err != nil {
		t.Fatalf("failed to unmarshal into: %v", err)
	}

	// Verify envelope
	if restoredProtocol.Type != TypeAke {
		t.Errorf("type mismatch: got %s, want %s", restoredProtocol.Type, TypeAke)
	}

	if restoredProtocol.SenderId != "envelope_sender" {
		t.Errorf("envelope senderId mismatch: got %s, want envelope_sender", restoredProtocol.SenderId)
	}

	// Verify payload
	if restoredAke.Round != AkeRound2 {
		t.Errorf("round mismatch: got %d, want %d", restoredAke.Round, AkeRound2)
	}

	if restoredAke.DhPk != "complex_dhpk_data" {
		t.Errorf("dhPk mismatch: got %s, want complex_dhpk_data", restoredAke.DhPk)
	}

	if restoredAke.SenderId != "test_sender" {
		t.Errorf("payload senderId mismatch: got %s, want test_sender", restoredAke.SenderId)
	}
}

// TestMessageProcessingLikeRealUsage tests message processing similar to main.go callback
func TestMessageProcessingLikeRealUsage(t *testing.T) {
	// Create an AKE message like it would be created in the real flow
	originalAke := AkeMessage{
		Round:    AkeRound1,
		DhPk:     "test_dhpk_from_caller",
		Proof:    "test_proof_from_caller",
		SenderId: "caller_id",
	}

	// Marshal it (this creates the protocol envelope)
	messageData, err := originalAke.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal AKE message: %v", err)
	}

	// Simulate the message processing from main.go callback
	// Step 1: Parse protocol message envelope
	var protocolMsg ProtocolMessage
	err = protocolMsg.Unmarshal(messageData)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	// Step 2: Check if it's an AKE message (like in main.go)
	if !protocolMsg.IsAke() {
		t.Fatal("expected AKE message")
	}

	// Step 3: Decode the AKE payload (like in main.go)
	var akeMsg AkeMessage
	err = protocolMsg.DecodePayload(&akeMsg)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Step 4: Check round and handle accordingly (like in main.go)
	if akeMsg.IsRoundOne() {
		// This is what would happen in the recipient's callback
		if akeMsg.DhPk != "test_dhpk_from_caller" {
			t.Errorf("dhPk mismatch in Round 1: got %s", akeMsg.DhPk)
		}

		if akeMsg.Proof != "test_proof_from_caller" {
			t.Errorf("proof mismatch in Round 1: got %s", akeMsg.Proof)
		}
	} else {
		t.Fatal("expected Round 1 message")
	}

	// Verify sender filtering (like in main.go)
	recipientSenderId := "recipient_id"
	if protocolMsg.SenderId == recipientSenderId {
		t.Fatal("should have filtered self-message")
	}
}

// TestJsonCompatibility tests JSON compatibility for debugging
func TestJsonCompatibility(t *testing.T) {
	akeMsg := AkeMessage{
		Round:    AkeRound1,
		DhPk:     "test_dhpk",
		Proof:    "test_proof",
		SenderId: "test_sender",
	}

	// Marshal using AkeMessage.Marshal (creates protocol envelope)
	envelopeData, err := akeMsg.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal AKE message: %v", err)
	}

	// The envelope should be valid JSON
	var jsonCheck map[string]interface{}
	err = json.Unmarshal(envelopeData, &jsonCheck)
	if err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}

	// Should have the expected envelope structure
	if jsonCheck["type"] != TypeAke {
		t.Errorf("JSON type mismatch: got %v, want %s", jsonCheck["type"], TypeAke)
	}

	if jsonCheck["sender_id"] != "test_sender" {
		t.Errorf("JSON sender_id mismatch: got %v, want test_sender", jsonCheck["sender_id"])
	}

	// Payload should be JSON too
	payload, ok := jsonCheck["payload"]
	if !ok {
		t.Fatal("missing payload in JSON")
	}

	payloadMap, ok := payload.(map[string]interface{})
	if !ok {
		t.Fatal("payload is not a JSON object")
	}

	if payloadMap["round"] != float64(AkeRound1) { // JSON numbers are float64
		t.Errorf("payload round mismatch: got %v, want %d", payloadMap["round"], AkeRound1)
	}
}
