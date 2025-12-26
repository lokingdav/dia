package protocol

import (
	"bytes"
	"testing"

	"github.com/dense-identity/denseid/internal/amf"
	"github.com/dense-identity/denseid/internal/bbs"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/encryption"
	"github.com/dense-identity/denseid/internal/helpers"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// testModeratorPk is a shared test moderator public key for RUA tests
// The .env files have a 64-byte moderator key which is incompatible with AMF (expects 32 bytes)
var testModeratorPk []byte

// testRuaExpiration is a valid future expiration for RUA testing (as proto bytes)
var testRuaExpiration []byte

func init() {
	// Generate a valid AMF moderator key for testing
	_, pk, _ := amf.Keygen()
	testModeratorPk = pk

	// Create a valid future expiration (30 days from now) as proto bytes
	exp := datetime.MakeExpiration(30)
	testRuaExpiration, _ = proto.Marshal(exp)
}

// createTestRtu creates a fresh RTU with valid signature for testing
func createTestRtu(state *CallState) *Rtu {
	// Create signature matching enrollment format:
	// message1: Hash(public_key, expiration, tn)
	// message2: name
	var tn string
	if state.IamCaller() {
		tn = state.Src
	} else {
		tn = state.Dst
	}

	message1 := helpers.HashAll(state.Config.RuaPublicKey, testRuaExpiration, []byte(tn))
	message2 := []byte(state.Config.MyName)
	signature, _ := bbs.Sign(testRaPrivateKey, [][]byte{message1, message2})

	return &Rtu{
		PublicKey:  state.Config.RuaPublicKey,
		Expiration: testRuaExpiration,
		Signature:  signature,
		Name:       state.Config.MyName,
	}
}

// setupAkeCompletedStates creates two call states that have completed the AKE phase
// Returns (callerState, recipientState) with matching shared keys
func setupAkeCompletedStates(t *testing.T, callerPhone, recipientPhone string) (*CallState, *CallState) {
	callerState := createTestCallStateForUser(callerPhone, recipientPhone, true)
	recipientState := createTestCallStateForUser(recipientPhone, callerPhone, false)

	// Initialize AKE for both parties (uses original RA public key for ZK proof verification)
	if err := InitAke(callerState); err != nil {
		t.Fatalf("failed to init AKE for caller: %v", err)
	}
	if err := InitAke(recipientState); err != nil {
		t.Fatalf("failed to init AKE for recipient: %v", err)
	}

	// Round 1: Caller sends AkeRequest
	round1Msg, err := AkeRequest(callerState)
	if err != nil {
		t.Fatalf("failed creating AkeRequest: %v", err)
	}

	protocolMsg1, err := UnmarshalMessage(round1Msg)
	if err != nil {
		t.Fatalf("failed to unmarshal AkeRequest: %v", err)
	}

	// Round 2: Recipient sends AkeResponse
	round2Ciphertext, err := AkeResponse(recipientState, protocolMsg1)
	if err != nil {
		t.Fatalf("failed creating AkeResponse: %v", err)
	}

	round2Plaintext, err := encryption.PkeDecrypt(callerState.Config.RuaPrivateKey, round2Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt AkeResponse: %v", err)
	}

	protocolMsg2, err := UnmarshalMessage(round2Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal AkeResponse: %v", err)
	}

	// Round 3: Caller sends AkeComplete
	round3Ciphertext, err := AkeComplete(callerState, protocolMsg2)
	if err != nil {
		t.Fatalf("failed creating AkeComplete: %v", err)
	}

	round3Plaintext, err := encryption.PkeDecrypt(recipientState.Config.RuaPrivateKey, round3Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt AkeComplete: %v", err)
	}

	protocolMsg3, err := UnmarshalMessage(round3Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal AkeComplete: %v", err)
	}

	// Finalize: Recipient processes AkeComplete
	if err := AkeFinalize(recipientState, protocolMsg3); err != nil {
		t.Fatalf("failed to finalize AKE: %v", err)
	}

	// Verify both have the same shared key
	if !bytes.Equal(callerState.SharedKey, recipientState.SharedKey) {
		t.Fatalf("AKE shared keys don't match")
	}

	// After AKE is complete, override keys for RUA testing:
	// - Override moderator key with a valid AMF key (original is 64 bytes, AMF needs 32)
	// - Override RA public key with test key so fresh RTU signatures can be verified
	callerState.Config.ModeratorPublicKey = testModeratorPk
	recipientState.Config.ModeratorPublicKey = testModeratorPk
	callerState.Config.RaPublicKey = testRaPublicKey
	recipientState.Config.RaPublicKey = testRaPublicKey

	return callerState, recipientState
}

// TestCompleteRuaFlowLikeRealUsage tests the complete RUA flow after AKE is completed
func TestCompleteRuaFlowLikeRealUsage(t *testing.T) {
	// Setup: Complete AKE first
	callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")

	// Store the AKE shared key for comparison
	akeSharedKey := make([]byte, len(callerState.SharedKey))
	copy(akeSharedKey, callerState.SharedKey)

	// Setup RTUs for both parties (simulating enrollment data)
	callerRtu := createTestRtu(callerState)
	recipientRtu := createTestRtu(recipientState)

	// Set call reason for caller
	callerState.CallReason = "Business inquiry"

	// === RUA Phase ===

	// Initialize RUA for both parties with their RTUs
	err := InitRTU(callerState, callerRtu)
	if err != nil {
		t.Fatalf("failed to init RTU for caller: %v", err)
	}

	err = InitRTU(recipientState, recipientRtu)
	if err != nil {
		t.Fatalf("failed to init RTU for recipient: %v", err)
	}

	// Verify RUA topics match (derived from same shared key)
	if !bytes.Equal(callerState.Rua.Topic, recipientState.Rua.Topic) {
		t.Fatalf("RUA topics don't match!\n\tCaller:    %x\n\tRecipient: %x",
			callerState.Rua.Topic, recipientState.Rua.Topic)
	}

	// === Round 1: Caller (Alice) -> Recipient (Bob) ===
	// Caller sends RuaRequest with RTU and AMF signature

	round1Ciphertext, err := RuaRequest(callerState)
	if err != nil {
		t.Fatalf("failed creating RuaRequest: %v", err)
	}

	// Recipient decrypts with shared symmetric key
	round1Plaintext, err := encryption.SymDecrypt(recipientState.SharedKey, round1Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt RuaRequest: %v", err)
	}

	protocolMsg1, err := UnmarshalMessage(round1Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal RuaRequest protocol message: %v", err)
	}

	// Verify it's a RuaRequest message
	if !IsRuaRequest(protocolMsg1) {
		t.Fatal("expected RuaRequest message")
	}

	// Skip self-messages check
	if protocolMsg1.SenderId == recipientState.SenderId {
		t.Fatal("received self-message, this shouldn't happen in test")
	}

	// Decode RUA payload
	ruaMsg1, err := DecodeRuaPayload(protocolMsg1)
	if err != nil {
		t.Fatalf("failed to decode RUA payload: %v", err)
	}

	// Verify DhPk is included
	if len(ruaMsg1.GetDhPk()) == 0 {
		t.Fatal("RuaRequest should contain DhPk")
	}

	// Verify caller's DhPk matches what was sent
	if !bytes.Equal(callerState.Rua.DhPk, ruaMsg1.GetDhPk()) {
		t.Fatal("caller DhPk do not match")
	}

	// Verify RTU is included
	if ruaMsg1.GetRtu() == nil {
		t.Fatal("RuaRequest should contain RTU")
	}

	// Verify call reason is included
	if ruaMsg1.GetReason() != callerState.CallReason {
		t.Fatalf("call reason mismatch: expected %s, got %s",
			callerState.CallReason, ruaMsg1.GetReason())
	}

	// Verify sigma (AMF signature) is included
	if len(ruaMsg1.GetSigma()) == 0 {
		t.Fatal("RuaRequest should contain AMF signature (sigma)")
	}

	// === Round 2: Recipient (Bob) -> Caller (Alice) ===
	// Bob verifies Alice's RTU and sends his RuaResponse

	round2Ciphertext, err := RuaResponse(recipientState, protocolMsg1)
	if err != nil {
		t.Fatalf("failed creating RuaResponse: %v", err)
	}

	// Verify recipient has computed a new shared key
	if len(recipientState.SharedKey) == 0 {
		t.Fatal("recipient shared key is empty after RuaResponse")
	}

	// The new shared key should be different from AKE shared key
	if bytes.Equal(recipientState.SharedKey, akeSharedKey) {
		t.Fatal("RUA shared key should be different from AKE shared key")
	}

	// Caller decrypts with shared symmetric key (still using AKE shared key)
	round2Plaintext, err := encryption.SymDecrypt(akeSharedKey, round2Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt RuaResponse: %v", err)
	}

	protocolMsg2, err := UnmarshalMessage(round2Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal RuaResponse protocol message: %v", err)
	}

	// Verify it's a RuaResponse message
	if !IsRuaResponse(protocolMsg2) {
		t.Fatal("expected RuaResponse message")
	}

	ruaMsg2, err := DecodeRuaPayload(protocolMsg2)
	if err != nil {
		t.Fatalf("failed to decode RuaResponse payload: %v", err)
	}

	// Verify Bob's DhPk is included
	if len(ruaMsg2.GetDhPk()) == 0 {
		t.Fatal("RuaResponse should contain Bob's DhPk")
	}

	// Verify Bob's DhPk matches his state
	if !bytes.Equal(recipientState.Rua.DhPk, ruaMsg2.GetDhPk()) {
		t.Fatal("recipient DhPk do not match")
	}

	// Verify Bob's RTU is included
	if ruaMsg2.GetRtu() == nil {
		t.Fatal("RuaResponse should contain RTU")
	}

	// Verify Bob's sigma is included
	if len(ruaMsg2.GetSigma()) == 0 {
		t.Fatal("RuaResponse should contain AMF signature (sigma)")
	}

	// === Finalization: Caller (Alice) processes RuaResponse ===
	// Alice verifies Bob's RTU and computes the final shared secret

	err = RuaFinalize(callerState, protocolMsg2)
	if err != nil {
		t.Fatalf("failed to finalize RUA for caller: %v", err)
	}

	// === Verification ===

	// Both parties should now have computed the same RUA shared secret
	if len(callerState.SharedKey) == 0 {
		t.Fatal("caller shared key is empty after RUA")
	}

	if len(recipientState.SharedKey) == 0 {
		t.Fatal("recipient shared key is empty after RUA")
	}

	// Verify final shared keys match
	if !bytes.Equal(callerState.SharedKey, recipientState.SharedKey) {
		t.Errorf("RUA shared keys don't match!\n\tCaller:    %x\n\tRecipient: %x",
			callerState.SharedKey, recipientState.SharedKey)
	}

	// Verify RUA shared key is different from AKE shared key
	if bytes.Equal(callerState.SharedKey, akeSharedKey) {
		t.Error("RUA shared key should be different from AKE shared key")
	}

	t.Logf("RUA completed successfully! RUA shared secret: %x", callerState.SharedKey)
}

// TestRuaRequest tests RuaRequest independently
func TestRuaRequest(t *testing.T) {
	callerState, _ := setupAkeCompletedStates(t, "alice", "bob")

	callerRtu := createTestRtu(callerState)
	callerState.CallReason = "Test call"

	err := InitRTU(callerState, callerRtu)
	if err != nil {
		t.Fatalf("failed to init RTU: %v", err)
	}

	ciphertext, err := RuaRequest(callerState)
	if err != nil {
		t.Fatalf("RuaRequest failed: %v", err)
	}

	if len(ciphertext) == 0 {
		t.Fatal("RuaRequest ciphertext is empty")
	}

	// Decrypt and verify
	plaintext, err := encryption.SymDecrypt(callerState.SharedKey, ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt RuaRequest: %v", err)
	}

	protocolMsg, err := UnmarshalMessage(plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal protocol message: %v", err)
	}

	if !IsRuaRequest(protocolMsg) {
		t.Fatal("expected RuaRequest message")
	}

	ruaMsg, err := DecodeRuaPayload(protocolMsg)
	if err != nil {
		t.Fatalf("failed to decode RUA payload: %v", err)
	}

	// Verify DhPk is included
	if len(ruaMsg.GetDhPk()) == 0 {
		t.Fatal("RuaRequest should contain DhPk")
	}

	// Verify RTU is included
	if ruaMsg.GetRtu() == nil {
		t.Fatal("RuaRequest should contain RTU")
	}

	// Verify sigma is included
	if len(ruaMsg.GetSigma()) == 0 {
		t.Fatal("RuaRequest should contain sigma")
	}

	// Verify topic is set
	if ruaMsg.GetTpc() == "" {
		t.Fatal("RuaRequest should contain topic")
	}

	// Verify call reason
	if ruaMsg.GetReason() != callerState.CallReason {
		t.Fatalf("call reason mismatch: expected %s, got %s",
			callerState.CallReason, ruaMsg.GetReason())
	}

	// Verify caller state was updated with Req
	if callerState.Rua.Req == nil {
		t.Fatal("caller Rua.Req should be set after RuaRequest")
	}
}

// TestVerifyRTU tests RTU verification independently
func TestVerifyRTU(t *testing.T) {
	callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")

	// Create valid RTU from caller's config
	callerRtu := createTestRtu(callerState)

	// Create a valid RuaMessage
	ruaMsg := &RuaMessage{
		DhPk:   []byte("test_dh_pk"),
		Tpc:    "test_topic",
		Reason: "test_reason",
		Rtu:    callerRtu,
	}

	// Sign the message
	ddA, err := MarshalDDA(ruaMsg)
	if err != nil {
		t.Fatalf("failed to marshal DDA: %v", err)
	}

	sigma, err := amf.Sign(
		callerState.Config.RuaPrivateKey,
		recipientState.Config.RuaPublicKey,
		callerState.Config.ModeratorPublicKey,
		ddA)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}
	ruaMsg.Sigma = sigma

	// Verify should pass with correct telephone number
	err = VerifyRTU(recipientState, callerState.Config.MyPhone, ruaMsg)
	if err != nil {
		t.Fatalf("VerifyRTU failed for valid RTU: %v", err)
	}
}

// TestVerifyRTUWithWrongTN tests that verification fails with wrong telephone number
func TestVerifyRTUWithWrongTN(t *testing.T) {
	callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")

	callerRtu := createTestRtu(callerState)

	ruaMsg := &RuaMessage{
		DhPk:   []byte("test_dh_pk"),
		Tpc:    "test_topic",
		Reason: "test_reason",
		Rtu:    callerRtu,
	}

	ddA, err := MarshalDDA(ruaMsg)
	if err != nil {
		t.Fatalf("failed to marshal DDA: %v", err)
	}

	sigma, err := amf.Sign(
		callerState.Config.RuaPrivateKey,
		recipientState.Config.RuaPublicKey,
		callerState.Config.ModeratorPublicKey,
		ddA)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}
	ruaMsg.Sigma = sigma

	// Verify should fail with wrong telephone number
	err = VerifyRTU(recipientState, "wrong_phone_number", ruaMsg)
	if err == nil {
		t.Fatal("VerifyRTU should fail with wrong telephone number")
	}
}

// TestRuaErrorCases tests error handling
func TestRuaErrorCases(t *testing.T) {
	t.Run("NilCallState_RuaRequest", func(t *testing.T) {
		_, err := RuaRequest(nil)
		if err == nil {
			t.Fatal("expected error for nil CallState")
		}
	})

	t.Run("NilCallState_RuaResponse", func(t *testing.T) {
		_, err := RuaResponse(nil, nil)
		if err == nil {
			t.Fatal("expected error for nil CallState")
		}
	})

	t.Run("NilMessage_RuaResponse", func(t *testing.T) {
		callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")
		_ = callerState

		recipientRtu := createTestRtu(recipientState)
		err := InitRTU(recipientState, recipientRtu)
		if err != nil {
			t.Fatalf("failed to init RTU: %v", err)
		}

		_, err = RuaResponse(recipientState, nil)
		if err == nil {
			t.Fatal("expected error for nil message")
		}
	})

	t.Run("WrongMessageType_RuaResponse", func(t *testing.T) {
		_, recipientState := setupAkeCompletedStates(t, "alice", "bob")

		recipientRtu := createTestRtu(recipientState)
		err := InitRTU(recipientState, recipientRtu)
		if err != nil {
			t.Fatalf("failed to init RTU: %v", err)
		}

		// Create protocol message with wrong type
		wrongProtocolMsg := &ProtocolMessage{
			Type:     TypeBye, // Wrong type
			SenderId: "test_sender",
		}

		_, err = RuaResponse(recipientState, wrongProtocolMsg)
		if err == nil {
			t.Fatal("expected error for wrong message type")
		}
	})

	t.Run("NilCallState_RuaFinalize", func(t *testing.T) {
		err := RuaFinalize(nil, nil)
		if err == nil {
			t.Fatal("expected error for nil CallState")
		}
	})

	t.Run("NilMessage_RuaFinalize", func(t *testing.T) {
		callerState, _ := setupAkeCompletedStates(t, "alice", "bob")

		err := RuaFinalize(callerState, nil)
		if err == nil {
			t.Fatal("expected error for nil message")
		}
	})

	t.Run("WrongMessageType_RuaFinalize", func(t *testing.T) {
		callerState, _ := setupAkeCompletedStates(t, "alice", "bob")

		// Create protocol message with wrong type
		wrongProtocolMsg := &ProtocolMessage{
			Type:     TypeRuaRequest, // Wrong type - should be RuaResponse
			SenderId: "test_sender",
		}

		err := RuaFinalize(callerState, wrongProtocolMsg)
		if err == nil {
			t.Fatal("expected error for wrong message type")
		}
	})

	t.Run("NilRuaMessage_VerifyRTU", func(t *testing.T) {
		callerState, _ := setupAkeCompletedStates(t, "alice", "bob")

		err := VerifyRTU(callerState, "test_tn", nil)
		if err == nil {
			t.Fatal("expected error for nil RuaMessage")
		}
	})

	t.Run("NilRtu_VerifyRTU", func(t *testing.T) {
		callerState, _ := setupAkeCompletedStates(t, "alice", "bob")

		ruaMsg := &RuaMessage{
			DhPk: []byte("test"),
			Rtu:  nil,
		}

		err := VerifyRTU(callerState, "test_tn", ruaMsg)
		if err == nil {
			t.Fatal("expected error for nil RTU")
		}
	})
}

// TestDeriveRuaTopic tests that RUA topic derivation is deterministic
func TestDeriveRuaTopic(t *testing.T) {
	callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")

	topic1 := DeriveRuaTopic(callerState)
	topic2 := DeriveRuaTopic(recipientState)

	if !bytes.Equal(topic1, topic2) {
		t.Errorf("RUA topics should be the same for both parties!\n\tCaller:    %x\n\tRecipient: %x",
			topic1, topic2)
	}

	// Call again to verify determinism
	topic1Again := DeriveRuaTopic(callerState)
	if !bytes.Equal(topic1, topic1Again) {
		t.Error("DeriveRuaTopic should be deterministic")
	}
}

// TestInitRTU tests RTU initialization
func TestInitRTU(t *testing.T) {
	callerState, _ := setupAkeCompletedStates(t, "alice", "bob")

	rtu := createTestRtu(callerState)

	err := InitRTU(callerState, rtu)
	if err != nil {
		t.Fatalf("InitRTU failed: %v", err)
	}

	// Verify DH keys were generated
	if len(callerState.Rua.DhSk) == 0 {
		t.Fatal("DhSk should be set after InitRTU")
	}

	if len(callerState.Rua.DhPk) == 0 {
		t.Fatal("DhPk should be set after InitRTU")
	}

	// Verify topic was set
	if len(callerState.Rua.Topic) == 0 {
		t.Fatal("Topic should be set after InitRTU")
	}

	// Verify RTU was stored
	if callerState.Rua.Rtu == nil {
		t.Fatal("Rtu should be set after InitRTU")
	}

	if !bytes.Equal(callerState.Rua.Rtu.PublicKey, rtu.PublicKey) {
		t.Fatal("RTU public key mismatch")
	}
}

// TestMarshalDDA tests the MarshalDDA function
func TestMarshalDDA(t *testing.T) {
	rtu := &Rtu{
		PublicKey:  []byte("test_pk"),
		Expiration: []byte("test_exp"),
		Signature:  []byte("test_sig"),
		Name:       "Test Name",
	}

	msg := &RuaMessage{
		DhPk:   []byte("test_dh_pk"),
		Tpc:    "test_topic",
		Reason: "test_reason",
		Rtu:    rtu,
		Sigma:  []byte("this_should_not_be_included"),
	}

	dda, err := MarshalDDA(msg)
	if err != nil {
		t.Fatalf("MarshalDDA failed: %v", err)
	}

	if len(dda) == 0 {
		t.Fatal("MarshalDDA returned empty bytes")
	}

	// Unmarshal and verify sigma is not included
	unmarshaled := &RuaMessage{}
	err = proto.Unmarshal(dda, unmarshaled)
	if err != nil {
		t.Fatalf("failed to unmarshal DDA: %v", err)
	}

	if len(unmarshaled.GetSigma()) != 0 {
		t.Fatal("DDA should not contain sigma")
	}

	// Verify other fields are preserved
	if !bytes.Equal(unmarshaled.GetDhPk(), msg.DhPk) {
		t.Fatal("DhPk should be preserved in DDA")
	}

	if unmarshaled.GetTpc() != msg.Tpc {
		t.Fatal("Tpc should be preserved in DDA")
	}

	if unmarshaled.GetReason() != msg.Reason {
		t.Fatal("Reason should be preserved in DDA")
	}

	if unmarshaled.GetRtu() == nil {
		t.Fatal("Rtu should be preserved in DDA")
	}
}

// TestRealEnrollmentDataRua tests RUA with actual enrollment data from .env files
func TestRealEnrollmentDataRua(t *testing.T) {
	// Load real configurations
	aliceConfig, err := loadConfigFromEnv("../../.env.alice")
	if err != nil {
		t.Fatalf("Failed to load .env.alice: %v", err)
	}

	bobConfig, err := loadConfigFromEnv("../../.env.bob")
	if err != nil {
		t.Fatalf("Failed to load .env.bob: %v", err)
	}

	// Verify they have the same RA public key
	if !bytes.Equal(aliceConfig.RaPublicKey, bobConfig.RaPublicKey) {
		t.Fatalf("Alice and Bob should have the same RA public key")
	}

	// Create call states (using original keys for AKE)
	aliceState := &CallState{
		Src:        aliceConfig.MyPhone,
		Dst:        bobConfig.MyPhone,
		Ts:         datetime.GetNormalizedTs(),
		IsOutgoing: true,
		SenderId:   uuid.NewString(),
		Ticket:     aliceConfig.SampleTicket,
		Config:     aliceConfig,
		CallReason: "Business meeting",
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

	// === Complete AKE Phase ===
	if err := InitAke(aliceState); err != nil {
		t.Fatalf("failed to init AKE for alice: %v", err)
	}
	if err := InitAke(bobState); err != nil {
		t.Fatalf("failed to init AKE for bob: %v", err)
	}

	// AKE Round 1
	round1Msg, err := AkeRequest(aliceState)
	if err != nil {
		t.Fatalf("AkeRequest failed: %v", err)
	}

	protocolMsg1, _ := UnmarshalMessage(round1Msg)

	// AKE Round 2
	round2Ciphertext, err := AkeResponse(bobState, protocolMsg1)
	if err != nil {
		t.Fatalf("AkeResponse failed: %v", err)
	}

	round2Plaintext, _ := encryption.PkeDecrypt(aliceState.Config.RuaPrivateKey, round2Ciphertext)
	protocolMsg2, _ := UnmarshalMessage(round2Plaintext)

	// AKE Round 3
	round3Ciphertext, err := AkeComplete(aliceState, protocolMsg2)
	if err != nil {
		t.Fatalf("AkeComplete failed: %v", err)
	}

	round3Plaintext, _ := encryption.PkeDecrypt(bobState.Config.RuaPrivateKey, round3Ciphertext)
	protocolMsg3, _ := UnmarshalMessage(round3Plaintext)

	// AKE Finalize
	if err := AkeFinalize(bobState, protocolMsg3); err != nil {
		t.Fatalf("AkeFinalize failed: %v", err)
	}

	// Verify AKE completed
	if !bytes.Equal(aliceState.SharedKey, bobState.SharedKey) {
		t.Fatalf("AKE shared keys don't match")
	}

	akeSharedKey := make([]byte, len(aliceState.SharedKey))
	copy(akeSharedKey, aliceState.SharedKey)

	t.Logf("AKE completed. Shared key: %x", akeSharedKey)

	// === RUA Phase ===
	// Override keys for RUA testing after AKE is complete
	aliceConfig.ModeratorPublicKey = testModeratorPk
	bobConfig.ModeratorPublicKey = testModeratorPk
	aliceConfig.RaPublicKey = testRaPublicKey
	bobConfig.RaPublicKey = testRaPublicKey

	// Create RTUs from enrollment data
	aliceRtu := createTestRtu(aliceState)
	bobRtu := createTestRtu(bobState)

	// Initialize RUA
	if err := InitRTU(aliceState, aliceRtu); err != nil {
		t.Fatalf("failed to init RTU for alice: %v", err)
	}
	if err := InitRTU(bobState, bobRtu); err != nil {
		t.Fatalf("failed to init RTU for bob: %v", err)
	}

	// RUA Round 1: Alice sends RuaRequest
	ruaRound1Ciphertext, err := RuaRequest(aliceState)
	if err != nil {
		t.Fatalf("RuaRequest failed: %v", err)
	}

	ruaRound1Plaintext, err := encryption.SymDecrypt(bobState.SharedKey, ruaRound1Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt RuaRequest: %v", err)
	}

	ruaProtocolMsg1, err := UnmarshalMessage(ruaRound1Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal RuaRequest: %v", err)
	}

	if !IsRuaRequest(ruaProtocolMsg1) {
		t.Fatal("expected RuaRequest message")
	}

	// RUA Round 2: Bob sends RuaResponse
	ruaRound2Ciphertext, err := RuaResponse(bobState, ruaProtocolMsg1)
	if err != nil {
		t.Fatalf("RuaResponse failed: %v", err)
	}

	// Bob should now have a new shared key
	if bytes.Equal(bobState.SharedKey, akeSharedKey) {
		t.Fatal("Bob's shared key should be different after RuaResponse")
	}

	ruaRound2Plaintext, err := encryption.SymDecrypt(akeSharedKey, ruaRound2Ciphertext)
	if err != nil {
		t.Fatalf("failed to decrypt RuaResponse: %v", err)
	}

	ruaProtocolMsg2, err := UnmarshalMessage(ruaRound2Plaintext)
	if err != nil {
		t.Fatalf("failed to unmarshal RuaResponse: %v", err)
	}

	if !IsRuaResponse(ruaProtocolMsg2) {
		t.Fatal("expected RuaResponse message")
	}

	// RUA Finalize: Alice processes RuaResponse
	err = RuaFinalize(aliceState, ruaProtocolMsg2)
	if err != nil {
		t.Fatalf("RuaFinalize failed: %v", err)
	}

	// Verify both have the same RUA shared key
	if !bytes.Equal(aliceState.SharedKey, bobState.SharedKey) {
		t.Errorf("RUA shared keys don't match!\n\tAlice: %x\n\tBob:   %x",
			aliceState.SharedKey, bobState.SharedKey)
	}

	// Verify RUA shared key is different from AKE shared key
	if bytes.Equal(aliceState.SharedKey, akeSharedKey) {
		t.Error("RUA shared key should be different from AKE shared key")
	}

	t.Logf("RUA completed with real enrollment data! RUA shared key: %x", aliceState.SharedKey)
}
