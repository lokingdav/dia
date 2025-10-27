package protocol

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/caarlos0/env/v11"
	keypb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	"github.com/dense-identity/denseid/internal/bbs"
	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	dia "github.com/lokingdav/libdia/bindings/go"
	"google.golang.org/grpc"
)

// mockKeyDerivationClient simulates the key derivation service
type mockKeyDerivationClient struct {
	evaluatedElement []byte
	shouldError      bool
	errorMsg         string
}

func (m *mockKeyDerivationClient) Evaluate(ctx context.Context, req *keypb.EvaluateRequest, opts ...grpc.CallOption) (*keypb.EvaluateResponse, error) {
	if m.shouldError {
		return nil, &mockError{msg: m.errorMsg}
	}

	// Return deterministic evaluation for testing
	if m.evaluatedElement == nil {
		// Default test evaluation (32 bytes)
		m.evaluatedElement = []byte("test_evaluated_element_32bytes__")
	}

	return &keypb.EvaluateResponse{
		EvaluatedElement: m.evaluatedElement,
	}, nil
}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

// Global test keys to ensure consistency across test parties
var (
	testRaPrivateKey []byte
	testRaPublicKey  []byte
	testRuaPublicKey []byte
	testExpiration   []byte
)

func init() {
	// Initialize shared test keys once
	testRaPrivateKey, testRaPublicKey, _ = bbs.Keygen()
	_, testRuaPublicKey, _ = bbs.Keygen()
	testExpiration = []byte("20991231235959Z")
}

// loadConfigFromEnv loads a SubscriberConfig from the specified .env file
func loadConfigFromEnv(envFile string) (*config.SubscriberConfig, error) {
	// Save current env
	originalEnv := make(map[string]string)
	for _, e := range os.Environ() {
		if pair := strings.SplitN(e, "=", 2); len(pair) == 2 {
			originalEnv[pair[0]] = pair[1]
		}
	}

	// Load from .env file
	err := godotenv.Load(envFile)
	if err != nil {
		return nil, err
	}

	// Parse config
	cfg := &config.SubscriberConfig{}
	err = env.Parse(cfg)
	if err != nil {
		return nil, err
	}

	err = cfg.ParseKeysAsBytes()
	if err != nil {
		return nil, err
	}

	// Restore original env
	os.Clearenv()
	for k, v := range originalEnv {
		os.Setenv(k, v)
	}

	return cfg, nil
}

// createTestCallStateForUser creates a CallState for a specific user identity
func createTestCallStateForUser(myPhone, otherPhone string, outgoing bool) *CallState {
	// Determine roles
	var callerId, recipient string
	if outgoing {
		callerId = myPhone
		recipient = otherPhone
	} else {
		callerId = otherPhone
		recipient = myPhone
	}

	config, err := loadConfigFromEnv(fmt.Sprintf("../../.env.%s", myPhone))

	if err != nil {
		panic(fmt.Sprintf("failed to load env for %s: %v", myPhone, err))
	}

	return &CallState{
		CallerId:   callerId,
		Recipient:  recipient,
		Ts:         datetime.GetNormalizedTs(),
		IsOutgoing: outgoing,
		SenderId:   uuid.NewString(),
		Ticket:     config.SampleTicket,
		Config:     config,
	}
}

// createTestCallState creates a CallState for testing, similar to main.go
func createTestCallState(phone string, outgoing bool) *CallState {
	// For backward compatibility - always creates state for "alice"
	return createTestCallStateForUser("alice", phone, outgoing)
}

// TestCompleteAkeFlowLikeRealUsage tests the complete AKE flow as used in main.go
func TestCompleteAkeFlowLikeRealUsage(t *testing.T) {
	// Create caller and recipient states with different identities
	// Caller "alice" calling recipient "bob"
	callerState := createTestCallStateForUser("alice", "bob", true)     // alice calls bob
	recipientState := createTestCallStateForUser("bob", "alice", false) // bob receives call from alice

	// Ensure both have same shared key from key derivation
	// In real usage, both parties would derive the same key from VOPRF
	// For testing, we'll manually set the same key
	testSharedKey := []byte("shared_test_key_for_both_parties")
	callerState.SetSharedKey(testSharedKey)
	recipientState.SetSharedKey(testSharedKey)

	// Verify both parties have same shared key
	if string(callerState.SharedKey) != string(recipientState.SharedKey) {
		t.Fatal("derived keys don't match between parties")
	}

	// Initialize AKE for both parties (like RunAuthenticatedKeyExchange in main.go)
	var err error
	err = InitAke(callerState)
	if err != nil {
		t.Fatalf("failed to init AKE for caller: %v", err)
	}

	err = InitAke(recipientState)
	if err != nil {
		t.Fatalf("failed to init AKE for recipient: %v", err)
	}

	// === Round 1: Caller -> Recipient ===

	// Caller creates Round 1 message
	round1Ciphertext, err := AkeInitCallerToRecipient(callerState)
	if err != nil {
		t.Fatalf("failed creating Round 1 message: %v", err)
	}

	// Simulate message transmission and processing (like the callback in main.go)
	round1Plaintext, err := encryption.SymDecrypt(recipientState.SharedKey, round1Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt Round 1 message: %v", err)
	}

	// Parse the protocol message
	var protocolMsg ProtocolMessage
	err = protocolMsg.Unmarshal(round1Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	// Verify it's an AkeInit message
	if !protocolMsg.IsAkeInit() {
		t.Fatal("expected AkeInit message")
	}

	// Skip self-messages (like in main.go)
	if protocolMsg.SenderId == recipientState.SenderId {
		t.Fatal("received self-message, this shouldn't happen in test")
	}

	// Decode AKE payload
	var akeMsg AkeMessage
	err = protocolMsg.DecodePayload(&akeMsg)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Verify retrieved DhPk equals the one sent
	if string(callerState.DhPk) != string(akeMsg.GetDhPk()) {
		t.Fatal("caller dhpk do not match")
	}

	// verify retrieved proof equals the one sent
	if string(callerState.Proof) != string(akeMsg.GetProof()) {
		// fmt.Printf("before: %s\nAfter: %s", callerState.Proof)
		t.Fatal("caller proof do not match")
	}

	// === Round 2: Recipient -> Caller ===

	// Recipient processes Round 1 and creates Round 2 response
	round2Ciphertext, err := AkeResponseRecipientToCaller(recipientState, &protocolMsg)
	if err != nil {
		t.Fatalf("failed creating Round 2 response: %v", err)
	}

	// Simulate Round 2 message transmission
	round2Plaintext, err := encryption.SymDecrypt(callerState.SharedKey, round2Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt Round 2 message: %v", err)
	}

	// Parse Round 2 protocol message
	var protocolMsg2 ProtocolMessage
	err = protocolMsg2.Unmarshal(round2Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal Round 2 protocol message: %v", err)
	}

	// Verify and decode AkeResponse message
	if !protocolMsg2.IsAkeResponse() {
		t.Fatal("expected AkeResponse message")
	}

	var akeMsg2 AkeMessage
	err = protocolMsg2.DecodePayload(&akeMsg2)
	if err != nil {
		t.Fatalf("failed to decode AkeResponse payload: %v", err)
	}

	// === Finalization: Caller processes AkeResponse ===

	err = AkeFinalizeCaller(callerState, &protocolMsg2)
	if err != nil {
		t.Fatalf("failed to finalize AKE: %v", err)
	}

	// === Verification ===

	// Both parties should now have computed the same final shared secret
	if len(callerState.SharedKey) == 0 {
		t.Fatal("caller shared key is empty after AKE")
	}

	if len(recipientState.SharedKey) == 0 {
		t.Fatal("recipient shared key is empty after AKE")
	}

	// Verify final shared keys match (should be the same after AKE completion)
	if string(callerState.SharedKey) != string(recipientState.SharedKey) {
		t.Errorf("final shared keys don't match!\n\tCaller:    %x\n\tRecipient: %x",
			callerState.SharedKey, recipientState.SharedKey)
	}

	t.Logf("AKE completed successfully! Shared secret: %x", callerState.SharedKey)
}

// TestAkeInitCallerToRecipient tests AkeInit independently
func TestAkeInitCallerToRecipient(t *testing.T) {
	callerState := createTestCallState("alice", true)

	err := InitAke(callerState)
	if err != nil {
		t.Fatalf("failed to init AKE: %v", err)
	}

	ciphertext, err := AkeInitCallerToRecipient(callerState)
	if err != nil {
		t.Fatalf("AkeInit failed: %v", err)
	}

	if len(ciphertext) == 0 {
		t.Fatal("AkeInit ciphertext is empty")
	}

	// Verify we can decrypt and parse the message
	plaintext, err := encryption.SymDecrypt(callerState.SharedKey, ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt AkeInit message: %v", err)
	}

	var protocolMsg ProtocolMessage
	err = protocolMsg.Unmarshal(plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	// Verify it's an AkeInit message
	if !protocolMsg.IsAkeInit() {
		t.Fatal("expected AkeInit message")
	}

	var akeMsg AkeMessage
	err = protocolMsg.DecodePayload(&akeMsg)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Verify retrieved DhPk equals the one sent
	if string(callerState.DhPk) != string(akeMsg.GetDhPk()) {
		t.Fatal("caller dhpk do not match")
	}
}

// TestAkeErrorCases tests error handling
func TestAkeErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("NilCallState", func(t *testing.T) {
		_, err := AkeInitCallerToRecipient(nil)
		if err == nil {
			t.Fatal("expected error for nil CallState")
		}
	})

	t.Run("UninitializedAke", func(t *testing.T) {
		state := createTestCallState("bob", true)
		state.SetSharedKey([]byte("test_key"))
		// Don't call InitAke

		_, err := AkeInitCallerToRecipient(state)
		if err == nil {
			t.Fatal("expected error for uninitialized AKE")
		}
	})

	t.Run("KeyDerivationError", func(t *testing.T) {
		mockClient := &mockKeyDerivationClient{
			shouldError: true,
			errorMsg:    "mock key derivation error",
		}

		state := createTestCallState("bob", true)
		_, err := AkeDeriveKey(ctx, mockClient, state)
		if err == nil {
			t.Fatal("expected key derivation error")
		}

		if err.Error() != "mock key derivation error" {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("WrongMessageType", func(t *testing.T) {
		recipientState := createTestCallState("alice", false)

		// Create protocol message with wrong type
		wrongProtocolMsg := &ProtocolMessage{
			Type:     TypeBye, // Wrong type - function expects AkeInit
			SenderId: "test_sender",
		}

		_, err := AkeResponseRecipientToCaller(recipientState, wrongProtocolMsg)
		if err == nil {
			t.Fatal("expected error for wrong message type")
		}

		if err.Error() != "AkeResponseRecipientToCaller can only be called on AkeInit message" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// TestRealEnrollmentData tests with actual enrollment data from .env files
func TestRealEnrollmentData(t *testing.T) {
	// Load real configurations from .env files (relative to project root)
	aliceConfig, err := loadConfigFromEnv("../../.env.alice")
	if err != nil {
		t.Fatalf("Failed to load .env.alice: %v", err)
	}

	bobConfig, err := loadConfigFromEnv("../../.env.bob")
	if err != nil {
		t.Fatalf("Failed to load .env.bob: %v", err)
	}

	// Verify that they have the same RA public key (shared trust anchor)
	if !bytes.Equal(aliceConfig.RaPublicKey, bobConfig.RaPublicKey) {
		t.Fatalf("Alice and Bob should have the same RA public key")
	}

	// Let's test whether we can verify the real enrollment signatures directly
	t.Run("VerifyRealEnrollmentSignatures", func(t *testing.T) {
		// Test Alice's signature
		aliceMessage1 := helpers.HashAll(aliceConfig.RuaPublicKey, aliceConfig.EnExpiration, []byte(aliceConfig.MyPhone))
		aliceMessage2 := []byte(aliceConfig.MyName)
		aliceValid, err := dia.BBSVerify([][]byte{aliceMessage1, aliceMessage2}, aliceConfig.RaPublicKey, aliceConfig.RaSignature)
		t.Logf("Alice signature verification: valid=%v, error=%v", aliceValid, err)
		if !aliceValid {
			t.Errorf("Alice's enrollment signature should be valid")
		}

		// Test Bob's signature
		bobMessage1 := helpers.HashAll(bobConfig.RuaPublicKey, bobConfig.EnExpiration, []byte(bobConfig.MyPhone))
		bobMessage2 := []byte(bobConfig.MyName)
		bobValid, err := dia.BBSVerify([][]byte{bobMessage1, bobMessage2}, bobConfig.RaPublicKey, bobConfig.RaSignature)
		t.Logf("Bob signature verification: valid=%v, error=%v", bobValid, err)
		if !bobValid {
			t.Errorf("Bob's enrollment signature should be valid")
		}
	})

	// Create call states like the real application
	aliceState := &CallState{
		CallerId:   aliceConfig.MyPhone,
		Recipient:  bobConfig.MyPhone,
		Ts:         datetime.GetNormalizedTs(),
		IsOutgoing: true,
		SenderId:   uuid.NewString(),
		Ticket:     aliceConfig.SampleTicket,
		Config:     aliceConfig,
	}

	bobState := &CallState{
		CallerId:   aliceConfig.MyPhone,
		Recipient:  bobConfig.MyPhone,
		Ts:         datetime.GetNormalizedTs(),
		IsOutgoing: false,
		SenderId:   uuid.NewString(),
		Ticket:     bobConfig.SampleTicket,
		Config:     bobConfig,
	}

	// Test proof creation and verification with real data
	testSharedKey := []byte("shared_test_key_for_both_parties")
	aliceState.SetSharedKey(testSharedKey)
	bobState.SetSharedKey(testSharedKey)

	// Initialize AKE
	err = InitAke(aliceState)
	if err != nil {
		t.Fatalf("failed to init AKE for alice: %v", err)
	}

	err = InitAke(bobState)
	if err != nil {
		t.Fatalf("failed to init AKE for bob: %v", err)
	}

	// Test Alice's Round 1 creation
	round1Ciphertext, err := AkeInitCallerToRecipient(aliceState)
	if err != nil {
		t.Fatalf("failed creating Round 1 message with real data: %v", err)
	}

	// Test Bob's Round 1 processing
	round1Plaintext, err := encryption.SymDecrypt(bobState.SharedKey, round1Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt Round 1 message: %v", err)
	}

	// Parse Alice's message like Bob would
	var protocolMsg ProtocolMessage
	err = protocolMsg.Unmarshal(round1Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	if !protocolMsg.IsAkeInit() {
		t.Fatal("expected AKE message")
	}

	var akeMsg AkeMessage
	err = protocolMsg.DecodePayload(&akeMsg)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// This should succeed with real enrollment data
	_, err = AkeResponseRecipientToCaller(bobState, &protocolMsg)
	if err != nil {
		t.Fatalf("Bob failed to process Alice's real enrollment proof: %v", err)
	}

	t.Log("Real enrollment data test passed!")
}
