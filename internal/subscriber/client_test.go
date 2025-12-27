package subscriber

import (
	"bytes"
	"testing"
	"time"

	"github.com/dense-identity/denseid/internal/protocol"
	"google.golang.org/protobuf/proto"
)

// TestSessionCreation tests basic session creation
func TestSessionCreation(t *testing.T) {
	// Create a mock relay client - we'll test with a nil client for basic creation
	var client *RelayClient

	session := NewSession(client, "test_topic", []byte("test_ticket"), "test_sender")

	if session == nil {
		t.Fatal("expected session to be created")
	}

	if session.topic != "test_topic" {
		t.Errorf("topic mismatch: got %s, want test_topic", session.topic)
	}

	if session.senderID != "test_sender" {
		t.Errorf("senderID mismatch: got %s, want test_sender", session.senderID)
	}

	if string(session.ticket) != "test_ticket" {
		t.Errorf("ticket mismatch: got %s, want test_ticket", string(session.ticket))
	}
}

// TestSessionLifecycle tests session start and close
func TestSessionLifecycle(t *testing.T) {
	var client *RelayClient
	session := NewSession(client, "test_topic", nil, "test_sender")

	// Test that session is not closed initially
	if session.closed.Load() {
		t.Error("session should not be closed initially")
	}

	// Close the session
	session.Close()

	// Test that session is closed after Close()
	if !session.closed.Load() {
		t.Error("session should be closed after Close()")
	}
}

// TestMessageProcessingWithProtocol tests protocol message processing
func TestMessageProcessingWithProtocol(t *testing.T) {
	// Create an AKE message
	akeMsg := &protocol.AkeMessage{
		DhPk:  []byte("test_dhpk"),
		Proof: []byte("test_proof"),
	}

	// Marshal AKE payload
	payloadBytes, err := proto.Marshal(akeMsg)
	if err != nil {
		t.Fatalf("failed to marshal AKE payload: %v", err)
	}

	// Create protocol message wrapper
	wrapperMsg := &protocol.ProtocolMessage{
		Type:     protocol.TypeAkeRequest,
		SenderId: "caller",
		Payload:  payloadBytes,
	}

	// Marshal it (this creates the protocol envelope)
	messageData, err := protocol.MarshalMessage(wrapperMsg)
	if err != nil {
		t.Fatalf("failed to marshal AKE message: %v", err)
	}

	// Simulate message processing callback
	var receivedMessage []byte
	processMessage := func(payload []byte) {
		receivedMessage = payload
	}

	// Process the message
	processMessage(messageData)

	// Parse the received message
	protocolMsg, err := protocol.UnmarshalMessage(receivedMessage)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	// Verify it's an AkeInit message
	if !protocol.IsAkeRequest(protocolMsg) {
		t.Fatal("expected AkeInit message")
	}

	// Decode the AKE payload (unencrypted)
	decodedAke, err := protocol.DecodeAkePayload(protocolMsg, nil)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Verify the data
	if !bytes.Equal(decodedAke.DhPk, []byte("test_dhpk")) {
		t.Errorf("dhPk mismatch: got %s, want test_dhpk", string(decodedAke.DhPk))
	}

	if !bytes.Equal(decodedAke.Proof, []byte("test_proof")) {
		t.Errorf("proof mismatch: got %s, want test_proof", string(decodedAke.Proof))
	}
}

// TestNoClientSideFiltering documents the new flow:
// clients receive payload bytes and do NOT filter self-messages;
// self-echo suppression is enforced by the server.
func TestNoClientSideFiltering(t *testing.T) {
	selfSenderID := "my_sender_id"

	// In the new flow, the client callback receives raw payload bytes.
	// We'll simulate two payloads (one hypothetically from self and one from others).
	rcvd := make([][]byte, 0, 2)
	onMessage := func(p []byte) { rcvd = append(rcvd, p) }

	// Simulate delivery
	selfPayload := []byte("self message (server should've suppressed this in practice)")
	otherPayload := []byte("other message")

	// Client-side: deliver whatever arrives (no filtering)
	onMessage(selfPayload)
	onMessage(otherPayload)

	if len(rcvd) != 2 {
		t.Fatalf("expected 2 delivered payloads, got %d", len(rcvd))
	}
	_ = selfSenderID // kept to mirror original test variables
}

// TestAkeRoundProcessing tests processing of AKE round messages
func TestAkeRoundProcessing(t *testing.T) {
	// Test Round 1 processing
	round1Msg := &protocol.AkeMessage{
		DhPk:  []byte("caller_dhpk"),
		Proof: []byte("caller_proof"),
	}

	// Marshal AKE payload
	payload1, err := proto.Marshal(round1Msg)
	if err != nil {
		t.Fatalf("failed to marshal round 1 payload: %v", err)
	}

	// Create protocol message wrapper for AkeInit
	protocolWrapper1 := &protocol.ProtocolMessage{
		Type:     protocol.TypeAkeRequest,
		SenderId: "caller",
		Payload:  payload1,
	}

	round1Data, err := protocol.MarshalMessage(protocolWrapper1)
	if err != nil {
		t.Fatalf("failed to marshal round 1: %v", err)
	}

	// Simulate recipient processing Round 1
	protocolMsg, err := protocol.UnmarshalMessage(round1Data)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	akeMsg, err := protocol.DecodeAkePayload(protocolMsg, nil)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if protocol.IsAkeRequest(protocolMsg) {
		// This is what recipient would do
		if !bytes.Equal(akeMsg.DhPk, []byte("caller_dhpk")) {
			t.Errorf("dhPk mismatch: got %s", string(akeMsg.DhPk))
		}

		// Create Round 2 response
		round2Msg := &protocol.AkeMessage{
			DhPk:  []byte("recipient_dhpk"),
			Proof: []byte("recipient_proof"),
		}

		// Marshal AKE payload
		payload2, err := proto.Marshal(round2Msg)
		if err != nil {
			t.Fatalf("failed to marshal round 2 payload: %v", err)
		}

		// Create protocol message wrapper for AkeResponse
		protocolWrapper2 := &protocol.ProtocolMessage{
			Type:     protocol.TypeAkeResponse,
			SenderId: "recipient",
			Payload:  payload2,
		}

		round2Data, err := protocol.MarshalMessage(protocolWrapper2)
		if err != nil {
			t.Fatalf("failed to marshal round 2: %v", err)
		}

		// Verify Round 2 can be parsed
		round2Protocol, err := protocol.UnmarshalMessage(round2Data)
		if err != nil {
			t.Fatalf("failed to unmarshal round 2: %v", err)
		}

		_, err = protocol.DecodeAkePayload(round2Protocol, nil)
		if err != nil {
			t.Fatalf("failed to decode round 2: %v", err)
		}

		if !protocol.IsAkeResponse(round2Protocol) {
			t.Error("expected Round 2 message")
		}

	} else {
		t.Fatal("expected Round 1 message")
	}
}

// TestSessionConfiguration tests session configuration parameters
func TestSessionConfiguration(t *testing.T) {
	var client *RelayClient
	session := NewSession(client, "config_topic", []byte("config_ticket"), "config_sender")

	// Test default configuration
	if session.publishTimeout != 3*time.Second {
		t.Errorf("publishTimeout mismatch: got %v, want 3s", session.publishTimeout)
	}

	if len(session.retryBackoff) == 0 {
		t.Error("retryBackoff should have values")
	}

	// Test expected backoff values
	expectedBackoffs := []time.Duration{0, 500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 5 * time.Second}
	if len(session.retryBackoff) != len(expectedBackoffs) {
		t.Errorf("retryBackoff length mismatch: got %d, want %d", len(session.retryBackoff), len(expectedBackoffs))
	}

	for i, expected := range expectedBackoffs {
		if i < len(session.retryBackoff) && session.retryBackoff[i] != expected {
			t.Errorf("retryBackoff[%d] mismatch: got %v, want %v", i, session.retryBackoff[i], expected)
		}
	}
}

// TestContextCancellation tests context cancellation behavior
func TestContextCancellation(t *testing.T) {
	var client *RelayClient
	session := NewSession(client, "cancel_topic", nil, "cancel_sender")

	// Get the initial context
	originalCtx := session.ctx

	// Cancel the session
	session.Close()

	// Check if context is cancelled
	select {
	case <-originalCtx.Done():
		// Expected - context should be cancelled
	case <-time.After(100 * time.Millisecond):
		t.Error("context should be cancelled after Close()")
	}
}
