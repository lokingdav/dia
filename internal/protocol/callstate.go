package protocol

import (
	"sync"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/dense-identity/denseid/internal/helpers"
	"github.com/google/uuid"
)

type CallState struct {
	mu                                       sync.Mutex
	IsOutgoing                               bool
	CallerId, Recipient, Ts, Topic, SenderId string
	DhSk, DhPk, Ticket, SharedKey, Chal0, Proof     []byte
	Config                                   *config.SubscriberConfig
}

func (s *CallState) GetAkeLabel() []byte {
	return []byte(s.CallerId + s.Ts)
}

func (s *CallState) MarshalTopic() []byte {
	b, _ := helpers.DecodeHex(s.Topic)
	return b
}

func (s *CallState) IamCaller() bool {
	return s.IsOutgoing
}

func (s *CallState) IamRecipient() bool {
	return !s.IsOutgoing
}

func (s *CallState) InitAke(dhSk, dhPk []byte, topic string) {
	s.mu.Lock()
	s.DhSk = dhSk
	s.DhPk = dhPk
	s.Topic = topic
	s.mu.Unlock()
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
