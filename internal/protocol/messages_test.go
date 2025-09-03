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
		DhPk:  "abcdef1234567890",
		Proof: "proof_data_here",
	}

	// Create protocol message wrapper
	protocolMsg := ProtocolMessage{
		Type:     TypeAke,
		Round:    AkeRound1,
		SenderId: "alice",
	}

	err := protocolMsg.SetPayload(original)
	if err != nil {
		t.Fatalf("failed to set payload: %v", err)
	}

	// Marshal
	data, err := protocolMsg.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal AKE message: %v", err)
	}

	// Parse the protocol message
	var restoredProtocol ProtocolMessage
	err = restoredProtocol.Unmarshal(data)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	if !restoredProtocol.IsAke() {
		t.Fatal("expected AKE message")
	}

	// Decode the AKE payload
	var restored AkeMessage
	err = restoredProtocol.DecodePayload(&restored)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Verify all fields
	if restoredProtocol.Round != AkeRound1 {
		t.Errorf("round mismatch: got %d, want %d", restoredProtocol.Round, AkeRound1)
	}

	if restored.DhPk != original.DhPk {
		t.Errorf("dhPk mismatch: got %s, want %s", restored.DhPk, original.DhPk)
	}

	if restored.Proof != original.Proof {
		t.Errorf("proof mismatch: got %s, want %s", restored.Proof, original.Proof)
	}

	// Test round detection
	if !restoredProtocol.IsRoundOne() {
		t.Error("should be round one")
	}

	if restoredProtocol.IsRoundTwo() {
		t.Error("should not be round two")
	}
}

// TestAkeMessageValidation tests AKE message validation
func TestAkeMessageValidation(t *testing.T) {
	testCases := []struct {
		name        string
		message     AkeMessage
		senderId    string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_message",
			message: AkeMessage{
				DhPk:  "valid_dhpk",
				Proof: "valid_proof",
			},
			senderId:    "valid_sender",
			expectError: false,
		},
		{
			name: "missing_dhpk",
			message: AkeMessage{
				DhPk:  "", // empty
				Proof: "valid_proof",
			},
			senderId:    "valid_sender",
			expectError: false, // No validation in current implementation
		},
		{
			name: "missing_proof",
			message: AkeMessage{
				DhPk:  "valid_dhpk",
				Proof: "", // empty
			},
			senderId:    "valid_sender",
			expectError: false, // No validation in current implementation
		},
		{
			name: "empty_sender_id",
			message: AkeMessage{
				DhPk:  "valid_dhpk",
				Proof: "valid_proof",
			},
			senderId:    "",    // empty
			expectError: false, // Empty SenderId is allowed at ProtocolMessage level
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create protocol message wrapper
			protocolMsg := ProtocolMessage{
				Type:     TypeAke,
				SenderId: tc.senderId,
			}

			err := protocolMsg.SetPayload(tc.message)
			if err != nil {
				t.Fatalf("failed to set payload: %v", err)
			}

			_, err = protocolMsg.Marshal()

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
	round1Msg := ProtocolMessage{Type: TypeAke, Round: AkeRound1}
	round2Msg := ProtocolMessage{Type: TypeAke, Round: AkeRound2}

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
		DhPk:  "complex_dhpk_data",
		Proof: "complex_proof_data",
	}

	protocolMsg := ProtocolMessage{
		Type:     TypeAke,
		Round:    AkeRound2,
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
	if restoredProtocol.Round != AkeRound2 {
		t.Errorf("round mismatch: got %d, want %d", restoredProtocol.Round, AkeRound2)
	}

	if restoredAke.DhPk != "complex_dhpk_data" {
		t.Errorf("dhPk mismatch: got %s, want complex_dhpk_data", restoredAke.DhPk)
	}

	// Note: SenderId is no longer part of AkeMessage - it's in the envelope
}

// TestMessageProcessingLikeRealUsage tests message processing similar to main.go callback
func TestMessageProcessingLikeRealUsage(t *testing.T) {
	// Create an AKE message like it would be created in the real flow
	originalAke := AkeMessage{
		DhPk:  "test_dhpk_from_caller",
		Proof: "test_proof_from_caller",
	}

	// Create protocol message wrapper
	protocolMsg := ProtocolMessage{
		Type:     TypeAke,
		Round:    AkeRound1,
		SenderId: "caller_id",
	}

	err := protocolMsg.SetPayload(originalAke)
	if err != nil {
		t.Fatalf("failed to set payload: %v", err)
	}

	// Marshal it (this creates the protocol envelope)
	messageData, err := protocolMsg.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal AKE message: %v", err)
	}

	// Simulate the message processing from main.go callback
	// Step 1: Parse protocol message envelope
	var receivedProtocolMsg ProtocolMessage
	err = receivedProtocolMsg.Unmarshal(messageData)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	// Step 2: Check if it's an AKE message (like in main.go)
	if !receivedProtocolMsg.IsAke() {
		t.Fatal("expected AKE message")
	}

	// Step 3: Decode the AKE payload (like in main.go)
	var akeMsg AkeMessage
	err = receivedProtocolMsg.DecodePayload(&akeMsg)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Step 4: Check round and handle accordingly (like in main.go)
	if receivedProtocolMsg.IsRoundOne() {
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
	if receivedProtocolMsg.SenderId == recipientSenderId {
		t.Fatal("should have filtered self-message")
	}
}

// TestJsonCompatibility tests JSON compatibility for debugging
func TestJsonCompatibility(t *testing.T) {
	akeMsg := AkeMessage{
		DhPk:  "test_dhpk",
		Proof: "test_proof",
	}

	// Create protocol message wrapper
	protocolMsg := ProtocolMessage{
		Type:     TypeAke,
		Round:    AkeRound1,
		SenderId: "test_sender",
	}

	err := protocolMsg.SetPayload(akeMsg)
	if err != nil {
		t.Fatalf("failed to set payload: %v", err)
	}

	// Marshal using ProtocolMessage.Marshal (creates protocol envelope)
	envelopeData, err := protocolMsg.Marshal()
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

	if jsonCheck["round"] != float64(AkeRound1) { // JSON numbers are float64
		t.Errorf("envelope round mismatch: got %v, want %d", jsonCheck["round"], AkeRound1)
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

	// Round should no longer be in payload, only in envelope
	if _, hasRound := payloadMap["round"]; hasRound {
		t.Error("payload should not contain round field - it should be in the envelope")
	}
}

// TestByeMessage tests bye message creation and detection
func TestByeMessage(t *testing.T) {
	// Create bye message
	byeData, err := CreateByeMessage("test_sender")
	if err != nil {
		t.Fatalf("failed to create bye message: %v", err)
	}

	// Parse bye message
	var byeMsg ProtocolMessage
	err = byeMsg.Unmarshal(byeData)
	if err != nil {
		t.Fatalf("failed to unmarshal bye message: %v", err)
	}

	// Verify it's a bye message
	if !byeMsg.IsBye() {
		t.Error("message should be detected as bye message")
	}

	if byeMsg.IsAke() {
		t.Error("bye message should not be detected as AKE message")
	}

	// Verify sender
	if byeMsg.SenderId != "test_sender" {
		t.Errorf("sender mismatch: got %s, want test_sender", byeMsg.SenderId)
	}

	// Verify type
	if byeMsg.Type != TypeBye {
		t.Errorf("type mismatch: got %s, want %s", byeMsg.Type, TypeBye)
	}
}
