package protocol

import (
	"sync"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/helpers"
	"github.com/google/uuid"
)

type CallState struct {
	mu                                     sync.Mutex
	IsOutgoing                             bool
	CallerId, Recipient, Ts, SenderId      string
	AkeTopic, RtuTopic, CurrentTopic       string // Explicit topics; CurrentTopic for filtering
	RtuActive                              bool   // Protocol phase flag
	DhSk, DhPk, Ticket, SharedKey, Chal0   []byte
	Proof                                  []byte
	Config                                 *config.SubscriberConfig
}

func (s *CallState) GetAkeLabel() []byte {
	return []byte(s.CallerId + s.Ts)
}

// MarshalAkeTopic returns the AKE topic in bytes (used by key derivation).
func (s *CallState) MarshalAkeTopic() []byte {
	b, _ := helpers.DecodeHex(s.AkeTopic)
	return b
}

// Optional helper if you ever need it.
func (s *CallState) MarshalRtuTopic() []byte {
	b, _ := helpers.DecodeHex(s.RtuTopic)
	return b
}

func (s *CallState) IamCaller() bool    { return s.IsOutgoing }
func (s *CallState) IamRecipient() bool { return !s.IsOutgoing }

func (s *CallState) InitAke(dhSk, dhPk []byte, akeTopic string) {
	s.mu.Lock()
	s.DhSk = dhSk
	s.DhPk = dhPk
	s.AkeTopic = akeTopic
	s.CurrentTopic = akeTopic // start on AKE topic
	s.RtuActive = false
	s.mu.Unlock()
}

// TransitionToRtu sets the RTU topic and flips the active topic to RTU.
// NOTE: AkeTopic is intentionally preserved so AkeComplete can still be sent on it.
func (s *CallState) TransitionToRtu(rtuTopic string) {
	s.mu.Lock()
	s.RtuTopic = rtuTopic
	s.CurrentTopic = rtuTopic
	s.RtuActive = true
	s.mu.Unlock()
}

func (s *CallState) GetCurrentTopic() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CurrentTopic
}

func (s *CallState) IsRtuActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.RtuActive
}

func (s *CallState) SetSharedKey(k []byte) {
	s.mu.Lock()
	s.SharedKey = k
	s.mu.Unlock()
}

func (s *CallState) UpdateR1(chal, proof []byte) {
	s.mu.Lock()
	s.Chal0 = chal
	s.Proof = proof
	s.mu.Unlock()
}

func NewCallState(config *config.SubscriberConfig, phoneNumber string, outgoing bool) CallState {
	var callerId, recipient string
	if outgoing {
		callerId = config.MyPhone
		recipient = phoneNumber
	} else {
		callerId = phoneNumber
		recipient = config.MyPhone
	}

	return CallState{
		CallerId:   callerId,
		Recipient:  recipient,
		Ts:         datetime.GetNormalizedTs(),
		IsOutgoing: outgoing,
		SenderId:   uuid.NewString(),
		Ticket:     config.SampleTicket,
		Config:     config,
	}
}
