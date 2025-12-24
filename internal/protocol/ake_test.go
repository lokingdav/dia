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
	var src, dst string
	if outgoing {
		src = myPhone
		dst = otherPhone
	} else {
		src = otherPhone
		dst = myPhone
	}

	config, err := loadConfigFromEnv(fmt.Sprintf("../../.env.%s", myPhone))

	if err != nil {
		panic(fmt.Sprintf("failed to load env for %s: %v", myPhone, err))
	}

	return &CallState{
		Src:        src,
		Dst:        dst,
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

	// Initialize AKE for both parties (generates DH keys for each)
	var err error
	err = InitAke(callerState)
	if err != nil {
		t.Fatalf("failed to init AKE for caller: %v", err)
	}

	err = InitAke(recipientState)
	if err != nil {
		t.Fatalf("failed to init AKE for recipient: %v", err)
	}

	// === Round 1: Caller (Alice) -> Recipient (Bob) ===
	// Alice sends ZK proof + her encryption public key (NO DhPk yet)

	round1Msg, err := AkeRequest(callerState)
	if err != nil {
		t.Fatalf("failed creating AkeRequest message: %v", err)
	}

	// AkeRequest returns plaintext (not encrypted in current implementation)
	var protocolMsg1 ProtocolMessage
	err = protocolMsg1.Unmarshal(round1Msg)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	// Verify it's an AkeRequest message
	if !protocolMsg1.IsAkeRequest() {
		t.Fatal("expected AkeRequest message")
	}

	// Skip self-messages (like in main.go)
	if protocolMsg1.SenderId == recipientState.SenderId {
		t.Fatal("received self-message, this shouldn't happen in test")
	}

	// Decode AKE payload
	var akeMsg1 AkeMessage
	err = protocolMsg1.DecodePayload(&akeMsg1)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// In new protocol, AkeRequest does NOT contain DhPk
	if len(akeMsg1.GetDhPk()) != 0 {
		t.Fatal("AkeRequest should not contain DhPk in new protocol")
	}

	// Verify proof is included
	if len(akeMsg1.GetProof()) == 0 {
		t.Fatal("AkeRequest should contain ZK proof")
	}

	// Verify caller's proof matches what was sent
	if !bytes.Equal(callerState.CallerProof, akeMsg1.GetProof()) {
		t.Fatal("caller proof do not match")
	}

	// === Round 2: Recipient (Bob) -> Caller (Alice) ===
	// Bob verifies Alice's ZK proof, sends his DhPk + ZK proof (encrypted with Alice's PK)

	round2Ciphertext, err := AkeResponse(recipientState, &protocolMsg1)
	if err != nil {
		t.Fatalf("failed creating AkeResponse: %v", err)
	}

	// Decrypt with Alice's private key (PkeDecrypt)
	round2Plaintext, err := encryption.PkeDecrypt(callerState.Config.RuaPrivateKey, round2Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt AkeResponse message: %v", err)
	}

	// Parse Round 2 protocol message
	var protocolMsg2 ProtocolMessage
	err = protocolMsg2.Unmarshal(round2Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal AkeResponse protocol message: %v", err)
	}

	// Verify it's an AkeResponse message
	if !protocolMsg2.IsAkeResponse() {
		t.Fatal("expected AkeResponse message")
	}

	var akeMsg2 AkeMessage
	err = protocolMsg2.DecodePayload(&akeMsg2)
	if err != nil {
		t.Fatalf("failed to decode AkeResponse payload: %v", err)
	}

	// Verify Bob's DhPk is included in AkeResponse
	if len(akeMsg2.GetDhPk()) == 0 {
		t.Fatal("AkeResponse should contain Bob's DhPk")
	}

	// Verify Bob's DhPk matches his state
	if !bytes.Equal(recipientState.DhPk, akeMsg2.GetDhPk()) {
		t.Fatal("recipient DhPk do not match")
	}

	// === Round 3: Caller (Alice) -> Recipient (Bob) ===
	// Alice computes shared secret, sends both DhPks (encrypted with Bob's PK)

	round3Ciphertext, err := AkeComplete(callerState, &protocolMsg2)
	if err != nil {
		t.Fatalf("failed to complete AKE: %v", err)
	}

	// Verify Alice has computed a shared key
	if len(callerState.SharedKey) == 0 {
		t.Fatal("caller shared key is empty after AkeComplete")
	}

	// Decrypt with Bob's private key (PkeDecrypt)
	round3Plaintext, err := encryption.PkeDecrypt(recipientState.Config.RuaPrivateKey, round3Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt AkeComplete message: %v", err)
	}

	// Parse Round 3 protocol message
	var protocolMsg3 ProtocolMessage
	err = protocolMsg3.Unmarshal(round3Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal AkeComplete protocol message: %v", err)
	}

	// Verify it's an AkeComplete message
	if !protocolMsg3.IsAkeComplete() {
		t.Fatal("expected AkeComplete message")
	}

	// === Finalization: Recipient (Bob) processes AkeComplete ===
	// Bob extracts Alice's DhPk and computes the same shared secret

	err = AkeFinalize(recipientState, &protocolMsg3)
	if err != nil {
		t.Fatalf("failed to finalize AKE for recipient: %v", err)
	}

	// === Verification ===

	// Both parties should now have computed the same final shared secret
	if len(callerState.SharedKey) == 0 {
		t.Fatal("caller shared key is empty after AKE")
	}

	if len(recipientState.SharedKey) == 0 {
		t.Fatal("recipient shared key is empty after AKE")
	}

	// Verify final shared keys match
	if !bytes.Equal(callerState.SharedKey, recipientState.SharedKey) {
		t.Errorf("final shared keys don't match!\n\tCaller:    %x\n\tRecipient: %x",
			callerState.SharedKey, recipientState.SharedKey)
	}

	t.Logf("AKE completed successfully! Shared secret: %x", callerState.SharedKey)
}

// TestAkeRequest tests AkeRequest independently
func TestAkeRequest(t *testing.T) {
	callerState := createTestCallState("bob", true)

	err := InitAke(callerState)
	if err != nil {
		t.Fatalf("failed to init AKE: %v", err)
	}

	msg, err := AkeRequest(callerState)
	if err != nil {
		t.Fatalf("AkeRequest failed: %v", err)
	}

	if len(msg) == 0 {
		t.Fatal("AkeRequest message is empty")
	}

	// AkeRequest returns plaintext message (not encrypted)
	var protocolMsg ProtocolMessage
	err = protocolMsg.Unmarshal(msg)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	// Verify it's an AkeRequest message
	if !protocolMsg.IsAkeRequest() {
		t.Fatal("expected AkeRequest message")
	}

	var akeMsg AkeMessage
	err = protocolMsg.DecodePayload(&akeMsg)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// In new protocol, AkeRequest does NOT contain DhPk
	if len(akeMsg.GetDhPk()) != 0 {
		t.Fatal("AkeRequest should not contain DhPk in new protocol")
	}

	// Verify ZK proof is included
	if len(akeMsg.GetProof()) == 0 {
		t.Fatal("AkeRequest should contain ZK proof")
	}

	// Verify public key is included
	if len(akeMsg.GetPublicKey()) == 0 {
		t.Fatal("AkeRequest should contain public key")
	}

	// Verify expiration is included
	if len(akeMsg.GetExpiration()) == 0 {
		t.Fatal("AkeRequest should contain expiration")
	}

	// Verify caller state was updated with challenge and proof
	if len(callerState.Chal0) == 0 {
		t.Fatal("caller Chal0 should be set after AkeRequest")
	}

	if len(callerState.CallerProof) == 0 {
		t.Fatal("caller CallerProof should be set after AkeRequest")
	}
}

// TestAkeErrorCases tests error handling
func TestAkeErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("NilCallState", func(t *testing.T) {
		_, err := AkeRequest(nil)
		if err == nil {
			t.Fatal("expected error for nil CallState")
		}
	})

	t.Run("UninitializedAke", func(t *testing.T) {
		state := createTestCallState("bob", true)
		state.SetSharedKey([]byte("test_key"))
		// Don't call InitAke

		_, err := AkeRequest(state)
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
		recipientState := createTestCallState("bob", false)
		err := InitAke(recipientState)
		if err != nil {
			t.Fatalf("failed to init AKE: %v", err)
		}

		// Create protocol message with wrong type
		wrongProtocolMsg := &ProtocolMessage{
			Type:     TypeBye, // Wrong type - function expects AkeRequest
			SenderId: "test_sender",
		}

		_, err = AkeResponse(recipientState, wrongProtocolMsg)
		if err == nil {
			t.Fatal("expected error for wrong message type")
		}

		if err.Error() != "AkeResponse can only be called on AkeRequest message" {
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
		Src:        aliceConfig.MyPhone,
		Dst:        bobConfig.MyPhone,
		Ts:         datetime.GetNormalizedTs(),
		IsOutgoing: true,
		SenderId:   uuid.NewString(),
		Ticket:     aliceConfig.SampleTicket,
		Config:     aliceConfig,
	}

	bobState := &CallState{
		Src:        aliceConfig.MyPhone,
		Dst:        bobConfig.MyPhone,
		Ts:         datetime.GetNormalizedTs(),
		IsOutgoing: false,
		SenderId:   uuid.NewString(),
		Ticket:     bobConfig.SampleTicket,
		Config:     bobConfig,
	}

	// Initialize AKE for both parties
	err = InitAke(aliceState)
	if err != nil {
		t.Fatalf("failed to init AKE for alice: %v", err)
	}

	err = InitAke(bobState)
	if err != nil {
		t.Fatalf("failed to init AKE for bob: %v", err)
	}

	// === Round 1: Alice sends AkeRequest ===
	round1Msg, err := AkeRequest(aliceState)
	if err != nil {
		t.Fatalf("failed creating AkeRequest with real data: %v", err)
	}

	// Parse Alice's message like Bob would
	var protocolMsg1 ProtocolMessage
	err = protocolMsg1.Unmarshal(round1Msg)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	if !protocolMsg1.IsAkeRequest() {
		t.Fatal("expected AkeRequest message")
	}

	var akeMsg1 AkeMessage
	err = protocolMsg1.DecodePayload(&akeMsg1)
	if err != nil {
		t.Fatalf("failed to decode AKE payload: %v", err)
	}

	// Verify AkeRequest does not contain DhPk in new protocol
	if len(akeMsg1.GetDhPk()) != 0 {
		t.Fatal("AkeRequest should not contain DhPk")
	}

	// === Round 2: Bob sends AkeResponse ===
	round2Ciphertext, err := AkeResponse(bobState, &protocolMsg1)
	if err != nil {
		t.Fatalf("Bob failed to process Alice's AkeRequest: %v", err)
	}

	// Decrypt with Alice's private key
	round2Plaintext, err := encryption.PkeDecrypt(aliceState.Config.RuaPrivateKey, round2Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt AkeResponse: %v", err)
	}

	var protocolMsg2 ProtocolMessage
	err = protocolMsg2.Unmarshal(round2Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal AkeResponse: %v", err)
	}

	if !protocolMsg2.IsAkeResponse() {
		t.Fatal("expected AkeResponse message")
	}

	// === Round 3: Alice sends AkeComplete ===
	round3Ciphertext, err := AkeComplete(aliceState, &protocolMsg2)
	if err != nil {
		t.Fatalf("Alice failed to complete AKE: %v", err)
	}

	// Verify Alice has a shared key
	if len(aliceState.SharedKey) == 0 {
		t.Fatal("Alice should have shared key after AkeComplete")
	}

	// Decrypt with Bob's private key
	round3Plaintext, err := encryption.PkeDecrypt(bobState.Config.RuaPrivateKey, round3Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt AkeComplete: %v", err)
	}

	var protocolMsg3 ProtocolMessage
	err = protocolMsg3.Unmarshal(round3Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal AkeComplete: %v", err)
	}

	if !protocolMsg3.IsAkeComplete() {
		t.Fatal("expected AkeComplete message")
	}

	// === Finalization: Bob processes AkeComplete ===
	err = AkeFinalize(bobState, &protocolMsg3)
	if err != nil {
		t.Fatalf("Bob failed to finalize AKE: %v", err)
	}

	// Verify Bob has a shared key
	if len(bobState.SharedKey) == 0 {
		t.Fatal("Bob should have shared key after AkeFinalize")
	}

	// Verify both have the same shared key
	if !bytes.Equal(aliceState.SharedKey, bobState.SharedKey) {
		t.Errorf("Shared keys don't match!\n\tAlice: %x\n\tBob:   %x",
			aliceState.SharedKey, bobState.SharedKey)
	}

	t.Logf("Real enrollment data test passed! Shared key: %x", aliceState.SharedKey)
}
