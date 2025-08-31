package protocol

import (
	"sync"

	"github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/datetime"
	"github.com/google/uuid"
)

type CallDetail struct {
	CallerId  string
	Recipient string
	Ts        string
}

type CallState struct {
	mu sync.Mutex
	CallDetail CallDetail
	IsCaller bool
	Topic string
	Ticket []byte
	SenderId string
	SharedKey []byte
	Config *config.SubscriberConfig
}

func (s *CallDetail) GetAkeLabel() []byte {
	return []byte(s.CallerId + s.Ts)
}

func (s *CallDetail) GetRtuLabel() []byte {
	return []byte(s.CallerId + s.Recipient + s.Ts)
}

func (s *CallState) SetTopic(v string) {
	s.mu.Lock()
	s.Topic = v
	s.mu.Unlock()
}

func (s *CallState) SetSharedKey(k []byte) {
	s.mu.Lock()
	s.SharedKey = k
	s.mu.Unlock()
}

func CreateCallState(config *config.SubscriberConfig, phone, action string) CallState {
	var callerId, recipient string
	var isCaller bool

	if action == "dial" {
		callerId = config.MyPhone
		recipient = phone
		isCaller = true
	} else {
		callerId = phone
		recipient = config.MyPhone
		isCaller = false
	}

	callDetail := CallDetail{
		CallerId:  callerId,
		Recipient: recipient,
		Ts:        datetime.GetNormalizedTs(),
	}

	return CallState{
		CallDetail: callDetail,
		IsCaller: isCaller,
		SenderId: uuid.NewString(),
		Ticket: config.SampleTicket,
		Config: config,
	}
}