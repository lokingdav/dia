package protocol

import (
	"errors"
	"sync"

	pb "github.com/dense-identity/denseid/api/go/protocol/v1"
	dr "github.com/status-im/doubleratchet"
)

// DrSession wraps a Double Ratchet session for secure messaging after AKE completes.
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

// InitDrSessionAsRecipient initializes a Double Ratchet session for the recipient.
// The recipient generates the initial DH key pair using their DR keys from enrollment.
// Called at the end of AkeFinalize.
func InitDrSessionAsRecipient(sessionId, sharedKey, drPrivateKey, drPublicKey []byte) (*DrSession, error) {
	if len(sharedKey) != 32 {
		return nil, errors.New("shared key must be 32 bytes")
	}
	if len(drPrivateKey) != 32 || len(drPublicKey) != 32 {
		return nil, errors.New("DR keys must be 32 bytes each")
	}

	sk := make(dr.Key, 32)
	copy(sk, sharedKey)

	privKey := make(dr.Key, 32)
	copy(privKey, drPrivateKey)

	pubKey := make(dr.Key, 32)
	copy(pubKey, drPublicKey)

	keyPair := drKeyPair{
		privateKey: privKey,
		publicKey:  pubKey,
	}

	session, err := dr.New(sessionId, sk, keyPair, nil)
	if err != nil {
		return nil, err
	}

	return &DrSession{session: session}, nil
}

// InitDrSessionAsCaller initializes a Double Ratchet session for the caller.
// The caller uses the recipient's DR public key from the AKE exchange.
// Called at the end of AkeComplete.
func InitDrSessionAsCaller(sessionId, sharedKey, remoteDrPk []byte) (*DrSession, error) {
	if len(sharedKey) != 32 {
		return nil, errors.New("shared key must be 32 bytes")
	}
	if len(remoteDrPk) != 32 {
		return nil, errors.New("remote DR public key must be 32 bytes")
	}

	sk := make(dr.Key, 32)
	copy(sk, sharedKey)

	remotePk := make(dr.Key, 32)
	copy(remotePk, remoteDrPk)

	session, err := dr.NewWithRemoteKey(sessionId, sk, remotePk, nil)
	if err != nil {
		return nil, err
	}

	return &DrSession{session: session}, nil
}

// DrEncrypt encrypts a message using the Double Ratchet session.
// Returns a proto DrMessage that can be serialized and sent.
func DrEncrypt(drSession *DrSession, plaintext []byte, associatedData []byte) (*pb.DrMessage, error) {
	if drSession == nil {
		return nil, errors.New("DR session not initialized")
	}

	drSession.mu.Lock()
	defer drSession.mu.Unlock()

	msg, err := drSession.session.RatchetEncrypt(plaintext, associatedData)
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
func DrDecrypt(drSession *DrSession, msg *pb.DrMessage, associatedData []byte) ([]byte, error) {
	if drSession == nil {
		return nil, errors.New("DR session not initialized")
	}
	if msg == nil || msg.Header == nil {
		return nil, errors.New("message cannot be nil")
	}

	drSession.mu.Lock()
	defer drSession.mu.Unlock()

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

	return drSession.session.RatchetDecrypt(drMsg, associatedData)
}
