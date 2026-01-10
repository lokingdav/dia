package subscriber

import (
	"fmt"

	dia "github.com/lokingdav/libdia/bindings/go/v2"
)

// Controller is a small wrapper combining Client + Session for convenience.
type Controller struct {
	Client  *RelayClient
	Session *Session
}

// ControllerConfig holds the configuration for creating a Controller
type ControllerConfig struct {
	RelayServerAddr string
	UseTLS          bool
}

// NewController builds a Client and Session in one step using DIA CallState.
func NewController(callState *dia.CallState, cfg *ControllerConfig) (*Controller, error) {
	if callState == nil || cfg == nil {
		return nil, fmt.Errorf("callState and config are required")
	}

	c, err := NewRelayClient(cfg.RelayServerAddr, cfg.UseTLS)
	if err != nil {
		return nil, err
	}

	initialTopic, err := callState.CurrentTopic()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to get current topic: %w", err)
	}

	ticket, err := callState.Ticket()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to get ticket: %w", err)
	}

	senderID, err := callState.SenderID()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to get sender ID: %w", err)
	}

	sess := NewSession(c, initialTopic, ticket, senderID)
	return &Controller{
		Client:  c,
		Session: sess,
	}, nil
}

// Start begins receiving; provide a function to handle inbound payload bytes.
func (c *Controller) Start(onMessage func([]byte)) {
	c.Session.Start(onMessage)
}

// Send publishes one payload (ticket is used only if topic creation is needed).
func (c *Controller) Send(payload []byte) error {
	return c.Session.Send(payload)
}

// SendToTopic publishes payload to a specific topic (optionally with ticket).
func (c *Controller) SendToTopic(topic string, payload []byte, ticket []byte) error {
	return c.Session.SendToTopic(topic, payload, ticket)
}

// SubscribeToNewTopicWithPayload lets you piggy-back a one-shot publish on subscribe.
func (c *Controller) SubscribeToNewTopicWithPayload(newTopic string, payload []byte, ticket []byte) error {
	return c.Session.SubscribeToNewTopicWithPayload(newTopic, payload, ticket)
}

// Close shuts down the session and the underlying client connection.
func (c *Controller) Close() error {
	c.Session.Close()
	return c.Client.Close()
}
