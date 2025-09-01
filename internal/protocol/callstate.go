package protocol

import (
	"sync"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/google/uuid"
)

type CallState struct {
	mu                                       sync.Mutex
	IsOutgoing                               bool
	CallerId, Recipient, Ts, Topic, SenderId string
	DhSk, DhPk, Ticket, SharedKey            []byte
	Config                                   *config.SubscriberConfig
}

func (s *CallState) GetAkeLabel() []byte {
	return []byte(s.CallerId + s.Ts)
}

func (s *CallState) GetRtuLabel() []byte {
	return []byte(s.CallerId + s.Recipient + s.Ts)
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
