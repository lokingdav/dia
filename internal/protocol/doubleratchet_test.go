package protocol

import (
	"bytes"
	"testing"

	pb "github.com/dense-identity/denseid/api/go/protocol/v1"
)

func TestDrSessionInitialization(t *testing.T) {
	// Setup: Complete AKE - DR is now initialized automatically at end of AKE
	callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")

	// Verify DR sessions are initialized after AKE
	if callerState.DrSession == nil {
		t.Fatal("caller DR session should be initialized after AkeComplete")
	}
	if recipientState.DrSession == nil {
		t.Fatal("recipient DR session should be initialized after AkeFinalize")
	}

	t.Log("DR sessions initialized automatically at end of AKE")
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
			msg, err := DrEncrypt(callerState.DrSession, []byte(tc.plaintext), tc.ad)
			if err != nil {
				t.Fatalf("DrEncrypt failed: %v", err)
			}

			// Recipient decrypts
			decrypted, err := DrDecrypt(recipientState.DrSession, msg, tc.ad)
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
		encrypted, err := DrEncrypt(callerState.DrSession, callerMsg, nil)
		if err != nil {
			t.Fatalf("round %d: DrEncrypt (caller->recipient) failed: %v", i, err)
		}
		decrypted, err := DrDecrypt(recipientState.DrSession, encrypted, nil)
		if err != nil {
			t.Fatalf("round %d: DrDecrypt (caller->recipient) failed: %v", i, err)
		}
		if !bytes.Equal(decrypted, callerMsg) {
			t.Errorf("round %d: caller->recipient message mismatch", i)
		}

		// Recipient -> Caller
		recipientMsg := []byte("hello from recipient " + string(rune('0'+i)))
		encrypted, err = DrEncrypt(recipientState.DrSession, recipientMsg, nil)
		if err != nil {
			t.Fatalf("round %d: DrEncrypt (recipient->caller) failed: %v", i, err)
		}
		decrypted, err = DrDecrypt(callerState.DrSession, encrypted, nil)
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
		msg, err := DrEncrypt(callerState.DrSession, []byte(pt), nil)
		if err != nil {
			t.Fatalf("DrEncrypt %d failed: %v", i, err)
		}
		messages[i] = msg
	}

	// Recipient decrypts all messages in order
	for i, msg := range messages {
		decrypted, err := DrDecrypt(recipientState.DrSession, msg, nil)
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
	t.Run("nil session encrypt", func(t *testing.T) {
		_, err := DrEncrypt(nil, []byte("test"), nil)
		if err == nil {
			t.Error("expected error for nil session")
		}
	})

	t.Run("nil session decrypt", func(t *testing.T) {
		_, err := DrDecrypt(nil, &pb.DrMessage{}, nil)
		if err == nil {
			t.Error("expected error for nil session")
		}
	})

	t.Run("nil message decrypt", func(t *testing.T) {
		callerState, _ := setupDrSessions(t)
		_, err := DrDecrypt(callerState.DrSession, nil, nil)
		if err == nil {
			t.Error("expected error for nil message")
		}
	})

	t.Run("invalid shared key length for recipient", func(t *testing.T) {
		_, err := InitDrSessionAsRecipient([]byte("sessionid"), []byte("short"), make([]byte, 32), make([]byte, 32))
		if err == nil {
			t.Error("expected error for invalid shared key length")
		}
	})

	t.Run("invalid DR key length for recipient", func(t *testing.T) {
		_, err := InitDrSessionAsRecipient([]byte("sessionid"), make([]byte, 32), []byte("short"), make([]byte, 32))
		if err == nil {
			t.Error("expected error for invalid DR key length")
		}
	})

	t.Run("invalid remote DR key length for caller", func(t *testing.T) {
		_, err := InitDrSessionAsCaller([]byte("sessionid"), make([]byte, 32), []byte("short"))
		if err == nil {
			t.Error("expected error for invalid remote DR key length")
		}
	})
}

// setupDrSessions is a helper that completes AKE (DR sessions are initialized as part of AKE)
func setupDrSessions(t *testing.T) (*CallState, *CallState) {
	t.Helper()

	callerState, recipientState := setupAkeCompletedStates(t, "alice", "bob")

	// Verify DR sessions are initialized
	if callerState.DrSession == nil {
		t.Fatal("caller DR session should be initialized after AKE")
	}
	if recipientState.DrSession == nil {
		t.Fatal("recipient DR session should be initialized after AKE")
	}

	return callerState, recipientState
}
