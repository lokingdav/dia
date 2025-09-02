package subscriber

import (
	"testing"
	"time"

	relaypb "github.com/dense-identity/denseid/api/go/relay/v1"
	"github.com/dense-identity/denseid/internal/protocol"
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
	akeMsg := protocol.AkeMessage{
		Round:    protocol.AkeRound1,
		DhPk:     "test_dhpk",
		Proof:    "test_proof",
		SenderId: "caller",
	}

	// Marshal it (this creates the protocol envelope)
	messageData, err := akeMsg.Marshal()
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
	var protocolMsg protocol.ProtocolMessage
	err = protocolMsg.Unmarshal(receivedMessage)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	// Verify it's an AKE message
	if !protocolMsg.IsAke() {
		t.Fatal("expected AKE message")
	}

	// Decode the AKE payload
	var decodedAke protocol.AkeMessage
	err = protocolMsg.DecodePayload(&decodedAke)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Verify the round and data
	if !decodedAke.IsRoundOne() {
		t.Error("expected Round 1 message")
	}

	if decodedAke.DhPk != "test_dhpk" {
		t.Errorf("dhPk mismatch: got %s, want test_dhpk", decodedAke.DhPk)
	}

	if decodedAke.Proof != "test_proof" {
		t.Errorf("proof mismatch: got %s, want test_proof", decodedAke.Proof)
	}
}

// TestMessageFiltering tests message filtering by sender ID
func TestMessageFiltering(t *testing.T) {
	selfSenderID := "my_sender_id"

	// Simulate filtering logic like in main.go
	filterSelfMessages := func(msg *relaypb.RelayMessage) bool {
		return msg.SenderId != selfSenderID
	}

	// Test self-message (should be filtered)
	selfMsg := &relaypb.RelayMessage{
		Topic:    "test_topic",
		Payload:  []byte("self message"),
		SenderId: selfSenderID,
		CorrId:   "self_corr",
	}

	if filterSelfMessages(selfMsg) {
		t.Error("self message should be filtered out")
	}

	// Test other-message (should pass)
	otherMsg := &relaypb.RelayMessage{
		Topic:    "test_topic",
		Payload:  []byte("other message"),
		SenderId: "other_sender",
		CorrId:   "other_corr",
	}

	if !filterSelfMessages(otherMsg) {
		t.Error("other message should not be filtered")
	}
}

// TestAkeRoundProcessing tests processing of AKE round messages
func TestAkeRoundProcessing(t *testing.T) {
	// Test Round 1 processing
	round1Msg := protocol.AkeMessage{
		Round:    protocol.AkeRound1,
		DhPk:     "caller_dhpk",
		Proof:    "caller_proof",
		SenderId: "caller",
	}

	round1Data, err := round1Msg.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal round 1: %v", err)
	}

	// Simulate recipient processing Round 1
	var protocolMsg protocol.ProtocolMessage
	err = protocolMsg.Unmarshal(round1Data)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	var akeMsg protocol.AkeMessage
	err = protocolMsg.DecodePayload(&akeMsg)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if akeMsg.IsRoundOne() {
		// This is what recipient would do
		if akeMsg.DhPk != "caller_dhpk" {
			t.Errorf("dhPk mismatch: got %s", akeMsg.DhPk)
		}

		// Create Round 2 response
		round2Msg := protocol.AkeMessage{
			Round:    protocol.AkeRound2,
			DhPk:     "recipient_dhpk",
			Proof:    "recipient_proof",
			SenderId: "recipient",
		}

		round2Data, err := round2Msg.Marshal()
		if err != nil {
			t.Fatalf("failed to marshal round 2: %v", err)
		}

		// Verify Round 2 can be parsed
		var round2Protocol protocol.ProtocolMessage
		err = round2Protocol.Unmarshal(round2Data)
		if err != nil {
			t.Fatalf("failed to unmarshal round 2: %v", err)
		}

		var round2Ake protocol.AkeMessage
		err = round2Protocol.DecodePayload(&round2Ake)
		if err != nil {
			t.Fatalf("failed to decode round 2: %v", err)
		}

		if !round2Ake.IsRoundTwo() {
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
