package protocol

import (
	"errors"
	"sync"

	pb "github.com/dense-identity/denseid/api/go/protocol/v1"
	dr "github.com/status-im/doubleratchet"
)

// DrSession wraps a Double Ratchet session for secure messaging after RUA completes.
type DrSession struct {
	mu      sync.Mutex
	session dr.Session
}

// drKeyPair implements doubleratchet.DHPair interface
type drKeyPair struct {
	privateKey dr.Key
	publicKey  dr.Key
}

func (k drKeyPair) PrivateKey() dr.Key { return k.privateKey }
func (k drKeyPair) PublicKey() dr.Key  { return k.publicKey }

// InitDrSessionAsRecipient initializes a Double Ratchet session for the party
// that will generate the initial DH key pair (typically the recipient).
// Returns the DR public key that must be sent to the other party.
func InitDrSessionAsRecipient(callState *CallState) ([]byte, error) {
	if callState == nil {
		return nil, errors.New("callState cannot be nil")
	}
	if len(callState.SharedKey) != 32 {
		return nil, errors.New("shared key must be 32 bytes")
	}

	sharedKey := make(dr.Key, 32)
	copy(sharedKey, callState.SharedKey)

	// Generate fresh DH key pair for Double Ratchet
	crypto := dr.DefaultCrypto{}
	keyPair, err := crypto.GenerateDH()
	if err != nil {
		return nil, err
	}

	sessionId := callState.Rua.Topic
	session, err := dr.New(sessionId, sharedKey, keyPair, nil)
	if err != nil {
		return nil, err
	}

	callState.DrSession = &DrSession{session: session}
	return keyPair.PublicKey(), nil
}

// InitDrSessionAsCaller initializes a Double Ratchet session for the party
// that receives the remote's DH public key (typically the caller).
func InitDrSessionAsCaller(callState *CallState, remoteDrPk []byte) error {
	if callState == nil {
		return errors.New("callState cannot be nil")
	}
	if len(callState.SharedKey) != 32 {
		return errors.New("shared key must be 32 bytes")
	}
	if len(remoteDrPk) != 32 {
		return errors.New("remote DR public key must be 32 bytes")
	}

	sharedKey := make(dr.Key, 32)
	copy(sharedKey, callState.SharedKey)

	remotePk := make(dr.Key, 32)
	copy(remotePk, remoteDrPk)

	sessionId := callState.Rua.Topic
	session, err := dr.NewWithRemoteKey(sessionId, sharedKey, remotePk, nil)
	if err != nil {
		return err
	}

	callState.DrSession = &DrSession{session: session}
	return nil
}

// DrEncrypt encrypts a message using the Double Ratchet session.
// Returns a proto DrMessage that can be serialized and sent.
func DrEncrypt(callState *CallState, plaintext []byte, associatedData []byte) (*pb.DrMessage, error) {
	if callState == nil || callState.DrSession == nil {
		return nil, errors.New("DR session not initialized")
	}

	callState.DrSession.mu.Lock()
	defer callState.DrSession.mu.Unlock()

	msg, err := callState.DrSession.session.RatchetEncrypt(plaintext, associatedData)
	if err != nil {
		return nil, err
	}

	return &pb.DrMessage{
		Header: &pb.DrHeader{
			Dh: msg.Header.DH,
			N:  msg.Header.N,
			Pn: msg.Header.PN,
		},
		Ciphertext: msg.Ciphertext,
	}, nil
}

// DrDecrypt decrypts a message using the Double Ratchet session.
func DrDecrypt(callState *CallState, msg *pb.DrMessage, associatedData []byte) ([]byte, error) {
	if callState == nil || callState.DrSession == nil {
		return nil, errors.New("DR session not initialized")
	}
	if msg == nil || msg.Header == nil {
		return nil, errors.New("message cannot be nil")
	}

	callState.DrSession.mu.Lock()
	defer callState.DrSession.mu.Unlock()

	// Convert proto DrMessage to doubleratchet.Message
	dhKey := make(dr.Key, len(msg.Header.Dh))
	copy(dhKey, msg.Header.Dh)

	drMsg := dr.Message{
		Header: dr.MessageHeader{
			DH: dhKey,
			N:  msg.Header.N,
			PN: msg.Header.Pn,
		},
		Ciphertext: msg.Ciphertext,
	}

	return callState.DrSession.session.RatchetDecrypt(drMsg, associatedData)
}
