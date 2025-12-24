package protocol

import (
	"sync"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/helpers"
	"github.com/google/uuid"
)

type CallState struct {
	mu                                   sync.Mutex
	IsOutgoing                           bool
	Src, Dst, Ts, SenderId, CallReason    string
	AkeTopic, RuaTopic, CurrentTopic     []byte
	RuaActive                            bool   // Protocol phase flag
	DhSk, DhPk, Ticket, SharedKey, Chal0 []byte
	CallerProof, RecipientProof          []byte
	Config                               *config.SubscriberConfig
}

func (s *CallState) GetAkeLabel() []byte {
	return []byte(s.Src + s.Ts)
}

func (s *CallState) GetAkeTopic() string { 
	return helpers.EncodeToHex(s.AkeTopic)
}

func (s *CallState) IamCaller() bool    { 
	return s.IsOutgoing 
}

func (s *CallState) IamRecipient() bool { 
	return !s.IsOutgoing 
}

func (s *CallState) InitAke(dhSk, dhPk []byte, akeTopic []byte) {
	s.mu.Lock()
	s.DhSk = dhSk
	s.DhPk = dhPk
	s.AkeTopic = akeTopic
	s.CurrentTopic = akeTopic // start on AKE topic
	s.RuaActive = false
	s.mu.Unlock()
}

// TransitionToRua sets the RUA topic and flips the active topic to RUA.
// NOTE: AkeTopic is intentionally preserved so AkeComplete can still be sent on it.
func (s *CallState) TransitionToRua(ruaTopic []byte) {
	s.mu.Lock()
	s.RuaTopic = ruaTopic
	s.CurrentTopic = ruaTopic
	s.RuaActive = true
	s.mu.Unlock()
}

func (s *CallState) GetCurrentTopic() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return helpers.EncodeToHex(s.CurrentTopic)
}

func (s *CallState) IsRuaActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.RuaActive
}

func (s *CallState) SetSharedKey(k []byte) {
	s.mu.Lock()
	s.SharedKey = k
	s.mu.Unlock()
}

func (s *CallState) UpdateCaller(chal, proof []byte) {
	s.mu.Lock()
	s.Chal0 = chal
	s.CallerProof = proof
	s.mu.Unlock()
}

func NewCallState(config *config.SubscriberConfig, phoneNumber string, outgoing bool) CallState {
	var src, dst string
	if outgoing {
		src = config.MyPhone
		dst = phoneNumber
	} else {
		src = phoneNumber
		dst = config.MyPhone
	}

	return CallState{
		Src:   src,
		Dst:  dst,
		Ts:         datetime.GetNormalizedTs(),
		IsOutgoing: outgoing,
		SenderId:   uuid.NewString(),
		Ticket:     config.SampleTicket,
		Config:     config,
	}
}
