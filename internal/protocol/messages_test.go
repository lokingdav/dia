package protocol

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
)

// TestProtocolMessageMarshalUnmarshal tests basic protocol message serialization
func TestProtocolMessageMarshalUnmarshal(t *testing.T) {
	// Create a test AKE payload
	akePayload := &AkeMessage{
		DhPk:  []byte("test_dhpk"),
		Proof: []byte("test_proof"),
	}

	payloadBytes, err := proto.Marshal(akePayload)
	if err != nil {
		t.Fatalf("failed to marshal AKE payload: %v", err)
	}

	original := &ProtocolMessage{
		Type:     TypeAkeRequest,
		SenderId: "test_sender_123",
		Topic:    "test_topic",
		Payload:  payloadBytes,
	}

	// Marshal
	data, err := MarshalMessage(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal
	restored, err := UnmarshalMessage(data)
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

	// Verify payload can be decoded (unencrypted)
	decodedPayload, err := DecodeAkePayload(restored, nil)
	if err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}

	if string(decodedPayload.DhPk) != "test_dhpk" {
		t.Errorf("DhPk mismatch: got %v, want test_dhpk", string(decodedPayload.DhPk))
	}

	if string(decodedPayload.Proof) != "test_proof" {
		t.Errorf("Proof mismatch: got %v, want test_proof", string(decodedPayload.Proof))
	}
}

// TestAkeMessageMarshalUnmarshal tests AKE message serialization
func TestAkeMessageMarshalUnmarshal(t *testing.T) {
	// Create test AKE message with raw bytes
	original := &AkeMessage{
		DhPk:  []byte("abcdef1234567890"),
		Proof: []byte("proof_data_here"),
	}

	// Marshal AKE payload
	payloadBytes, err := proto.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal AKE payload: %v", err)
	}

	// Create protocol message wrapper
	protocolMsg := &ProtocolMessage{
		Type:     TypeAkeRequest,
		SenderId: "alice",
		Topic:    "test_topic",
		Payload:  payloadBytes,
	}

	// Marshal
	data, err := MarshalMessage(protocolMsg)
	if err != nil {
		t.Fatalf("failed to marshal protocol message: %v", err)
	}

	// Parse the protocol message
	restoredProtocol, err := UnmarshalMessage(data)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	if !IsAkeRequest(restoredProtocol) {
		t.Fatal("expected AkeRequest message")
	}

	// Decode the AKE payload (unencrypted)
	restored, err := DecodeAkePayload(restoredProtocol, nil)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Verify all fields
	if !bytes.Equal(restored.DhPk, original.DhPk) {
		t.Errorf("dhPk mismatch: got %v, want %v", restored.DhPk, original.DhPk)
	}

	if !bytes.Equal(restored.Proof, original.Proof) {
		t.Errorf("proof mismatch: got %v, want %v", restored.Proof, original.Proof)
	}
}

// TestAkeMessageTypeHelpers tests the message type helper functions
func TestAkeMessageTypeHelpers(t *testing.T) {
	// Test AkeRequest message
	akeRequestMsg := &ProtocolMessage{Type: TypeAkeRequest}
	akeResponseMsg := &ProtocolMessage{Type: TypeAkeResponse}
	akeCompleteMsg := &ProtocolMessage{Type: TypeAkeComplete}
	byeMsg := &ProtocolMessage{Type: TypeBye}

	// Test AkeRequest
	if !IsAkeRequest(akeRequestMsg) {
		t.Error("should be AkeRequest")
	}
	if IsAkeResponse(akeRequestMsg) {
		t.Error("should not be AkeResponse")
	}
	if IsAkeComplete(akeRequestMsg) {
		t.Error("should not be AkeComplete")
	}

	// Test AkeResponse
	if IsAkeRequest(akeResponseMsg) {
		t.Error("should not be AkeRequest")
	}
	if !IsAkeResponse(akeResponseMsg) {
		t.Error("should be AkeResponse")
	}
	if IsAkeComplete(akeResponseMsg) {
		t.Error("should not be AkeComplete")
	}

	// Test AkeComplete
	if IsAkeRequest(akeCompleteMsg) {
		t.Error("should not be AkeRequest")
	}
	if IsAkeResponse(akeCompleteMsg) {
		t.Error("should not be AkeResponse")
	}
	if !IsAkeComplete(akeCompleteMsg) {
		t.Error("should be AkeComplete")
	}

	// Test Bye
	if IsAkeRequest(byeMsg) {
		t.Error("should not be AkeRequest")
	}
	if IsAkeResponse(byeMsg) {
		t.Error("should not be AkeResponse")
	}
	if IsAkeComplete(byeMsg) {
		t.Error("should not be AkeComplete")
	}
	if !IsBye(byeMsg) {
		t.Error("should be Bye")
	}
}

// TestProtocolMessageValidation tests protocol message validation
func TestProtocolMessageValidation(t *testing.T) {
	t.Run("nil_message", func(t *testing.T) {
		_, err := MarshalMessage(nil)
		if err == nil {
			t.Fatal("expected error for nil message")
		}
	})

	t.Run("missing_type", func(t *testing.T) {
		msg := &ProtocolMessage{
			SenderId: "test",
		}
		_, err := MarshalMessage(msg)
		if err == nil {
			t.Fatal("expected error for missing type")
		}
	})
}

// TestNilMessageTypeDetection tests type detection on nil messages
func TestNilMessageTypeDetection(t *testing.T) {
	var nilMsg *ProtocolMessage

	// Test nil message detection - should return false for all types
	if IsAkeRequest(nilMsg) || IsAkeResponse(nilMsg) || IsAkeComplete(nilMsg) || IsBye(nilMsg) {
		t.Error("nil message should not be detected as any type")
	}
}

// TestByeMessage tests bye message creation and detection
func TestByeMessage(t *testing.T) {
	// Create bye message
	byeData, err := CreateByeMessage("test_sender", "test_topic")
	if err != nil {
		t.Fatalf("failed to create bye message: %v", err)
	}

	// Parse bye message
	byeMsg, err := UnmarshalMessage(byeData)
	if err != nil {
		t.Fatalf("failed to unmarshal bye message: %v", err)
	}

	// Verify it's a bye message
	if !IsBye(byeMsg) {
		t.Error("message should be detected as bye message")
	}

	// Verify bye message is not detected as any AKE type
	if IsAkeRequest(byeMsg) || IsAkeResponse(byeMsg) || IsAkeComplete(byeMsg) {
		t.Error("bye message should not be detected as any AKE message type")
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
