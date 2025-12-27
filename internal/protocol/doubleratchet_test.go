package protocol

import (
	"bytes"
	"testing"

	pb "github.com/dense-identity/denseid/api/go/protocol/v1"
)

func TestDrSessionInitialization(t *testing.T) {
	// Setup: Complete AKE and RUA first
	callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")
	callerState.CallReason = "Test call"

	// Initialize RUA for both parties
	if err := InitRTU(callerState); err != nil {
		t.Fatalf("failed to init RTU for caller: %v", err)
	}
	if err := InitRTU(recipientState); err != nil {
		t.Fatalf("failed to init RTU for recipient: %v", err)
	}

	// RUA Round 1
	ruaRound1Msg, err := RuaRequest(callerState)
	if err != nil {
		t.Fatalf("RuaRequest failed: %v", err)
	}
	ruaProtocolMsg1, _ := UnmarshalMessage(ruaRound1Msg)

	// RUA Round 2
	ruaRound2Msg, err := RuaResponse(recipientState, ruaProtocolMsg1)
	if err != nil {
		t.Fatalf("RuaResponse failed: %v", err)
	}
	ruaProtocolMsg2, _ := UnmarshalMessage(ruaRound2Msg)

	// RUA Finalize
	if err := RuaFinalize(callerState, ruaProtocolMsg2); err != nil {
		t.Fatalf("RuaFinalize failed: %v", err)
	}

	// Verify shared keys match
	if !bytes.Equal(callerState.SharedKey, recipientState.SharedKey) {
		t.Fatal("RUA shared keys should match")
	}

	t.Logf("RUA shared key: %x", callerState.SharedKey)

	// Now initialize Double Ratchet sessions
	// Recipient initializes first and gets DR public key
	drPubKey, err := InitDrSessionAsRecipient(recipientState)
	if err != nil {
		t.Fatalf("InitDrSessionAsRecipient failed: %v", err)
	}
	if len(drPubKey) != 32 {
		t.Fatalf("DR public key should be 32 bytes, got %d", len(drPubKey))
	}

	// Caller initializes with recipient's DR public key
	if err := InitDrSessionAsCaller(callerState, drPubKey); err != nil {
		t.Fatalf("InitDrSessionAsCaller failed: %v", err)
	}

	// Verify both sessions are initialized
	if recipientState.DrSession == nil {
		t.Fatal("recipient DR session should be initialized")
	}
	if callerState.DrSession == nil {
		t.Fatal("caller DR session should be initialized")
	}

	t.Log("DR sessions initialized successfully")
}

func TestDrEncryptDecrypt(t *testing.T) {
	callerState, recipientState := setupDrSessions(t)

	testCases := []struct {
		name      string
		plaintext string
		ad        []byte
	}{
		{"simple message", "Hello, Bob!", nil},
		{"with associated data", "Secret message", []byte("metadata")},
		{"empty message", "", nil},
		{"binary data", string([]byte{0x00, 0x01, 0x02, 0xff}), nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Caller encrypts
			msg, err := DrEncrypt(callerState, []byte(tc.plaintext), tc.ad)
			if err != nil {
				t.Fatalf("DrEncrypt failed: %v", err)
			}

			// Recipient decrypts
			decrypted, err := DrDecrypt(recipientState, msg, tc.ad)
			if err != nil {
				t.Fatalf("DrDecrypt failed: %v", err)
			}

			if string(decrypted) != tc.plaintext {
				t.Errorf("decrypted message mismatch: got %q, want %q", string(decrypted), tc.plaintext)
			}
		})
	}
}

func TestDrBidirectionalCommunication(t *testing.T) {
	callerState, recipientState := setupDrSessions(t)

	for i := 0; i < 10; i++ {
		// Caller -> Recipient
		callerMsg := []byte("hello from caller " + string(rune('0'+i)))
		encrypted, err := DrEncrypt(callerState, callerMsg, nil)
		if err != nil {
			t.Fatalf("round %d: DrEncrypt (caller->recipient) failed: %v", i, err)
		}
		decrypted, err := DrDecrypt(recipientState, encrypted, nil)
		if err != nil {
			t.Fatalf("round %d: DrDecrypt (caller->recipient) failed: %v", i, err)
		}
		if !bytes.Equal(decrypted, callerMsg) {
			t.Errorf("round %d: caller->recipient message mismatch", i)
		}

		// Recipient -> Caller
		recipientMsg := []byte("hello from recipient " + string(rune('0'+i)))
		encrypted, err = DrEncrypt(recipientState, recipientMsg, nil)
		if err != nil {
			t.Fatalf("round %d: DrEncrypt (recipient->caller) failed: %v", i, err)
		}
		decrypted, err = DrDecrypt(callerState, encrypted, nil)
		if err != nil {
			t.Fatalf("round %d: DrDecrypt (recipient->caller) failed: %v", i, err)
		}
		if !bytes.Equal(decrypted, recipientMsg) {
			t.Errorf("round %d: recipient->caller message mismatch", i)
		}
	}

	t.Log("Bidirectional DR communication successful over 10 rounds")
}

func TestDrMultipleMessagesFromOneSide(t *testing.T) {
	callerState, recipientState := setupDrSessions(t)

	// Caller sends multiple messages in a row
	messages := make([]*pb.DrMessage, 5)
	plaintexts := []string{"msg1", "msg2", "msg3", "msg4", "msg5"}

	for i, pt := range plaintexts {
		msg, err := DrEncrypt(callerState, []byte(pt), nil)
		if err != nil {
			t.Fatalf("DrEncrypt %d failed: %v", i, err)
		}
		messages[i] = msg
	}

	// Recipient decrypts all messages in order
	for i, msg := range messages {
		decrypted, err := DrDecrypt(recipientState, msg, nil)
		if err != nil {
			t.Fatalf("DrDecrypt %d failed: %v", i, err)
		}
		if string(decrypted) != plaintexts[i] {
			t.Errorf("message %d mismatch: got %q, want %q", i, string(decrypted), plaintexts[i])
		}
	}

	t.Log("Multiple sequential messages decrypted successfully")
}

func TestDrErrorCases(t *testing.T) {
	t.Run("nil callstate encrypt", func(t *testing.T) {
		_, err := DrEncrypt(nil, []byte("test"), nil)
		if err == nil {
			t.Error("expected error for nil callstate")
		}
	})

	t.Run("nil callstate decrypt", func(t *testing.T) {
		_, err := DrDecrypt(nil, &pb.DrMessage{}, nil)
		if err == nil {
			t.Error("expected error for nil callstate")
		}
	})

	t.Run("uninitialized DR session encrypt", func(t *testing.T) {
		state := &CallState{}
		_, err := DrEncrypt(state, []byte("test"), nil)
		if err == nil {
			t.Error("expected error for uninitialized DR session")
		}
	})

	t.Run("nil message decrypt", func(t *testing.T) {
		callerState, _ := setupDrSessions(t)
		_, err := DrDecrypt(callerState, nil, nil)
		if err == nil {
			t.Error("expected error for nil message")
		}
	})

	t.Run("invalid shared key length", func(t *testing.T) {
		state := &CallState{SharedKey: []byte("short")}
		_, err := InitDrSessionAsRecipient(state)
		if err == nil {
			t.Error("expected error for invalid shared key length")
		}
	})

	t.Run("invalid remote DR key length", func(t *testing.T) {
		state := &CallState{SharedKey: make([]byte, 32)}
		err := InitDrSessionAsCaller(state, []byte("short"))
		if err == nil {
			t.Error("expected error for invalid remote DR key length")
		}
	})
}

// setupDrSessions is a helper that completes AKE, RUA, and initializes DR sessions
func setupDrSessions(t *testing.T) (*CallState, *CallState) {
	t.Helper()

	callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")
	callerState.CallReason = "Test"

	if err := InitRTU(callerState); err != nil {
		t.Fatalf("failed to init RTU for caller: %v", err)
	}
	if err := InitRTU(recipientState); err != nil {
		t.Fatalf("failed to init RTU for recipient: %v", err)
	}

	// RUA flow
	ruaRound1Msg, _ := RuaRequest(callerState)
	ruaProtocolMsg1, _ := UnmarshalMessage(ruaRound1Msg)
	ruaRound2Msg, _ := RuaResponse(recipientState, ruaProtocolMsg1)
	ruaProtocolMsg2, _ := UnmarshalMessage(ruaRound2Msg)
	RuaFinalize(callerState, ruaProtocolMsg2)

	// Initialize DR sessions
	drPubKey, err := InitDrSessionAsRecipient(recipientState)
	if err != nil {
		t.Fatalf("InitDrSessionAsRecipient failed: %v", err)
	}
	if err := InitDrSessionAsCaller(callerState, drPubKey); err != nil {
		t.Fatalf("InitDrSessionAsCaller failed: %v", err)
	}

	return callerState, recipientState
}
