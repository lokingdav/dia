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
	// InitialTopic overrides the call state's CurrentTopic() when non-empty.
	// This is useful when the caller wants to avoid subscribing to the AKE
	// topic (e.g., cache-hit RUA-only flows).
	InitialTopic string
	// InitialTicket overrides callState.Ticket() when non-nil and non-empty.
	// Must be compatible with InitialTopic.
	InitialTicket []byte
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

	initialTopic := cfg.InitialTopic
	if initialTopic == "" {
		var err error
		initialTopic, err = callState.CurrentTopic()
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("failed to get current topic: %w", err)
		}
	}

	ticket := cfg.InitialTicket
	if len(ticket) == 0 {
		var err error
		ticket, err = callState.Ticket()
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("failed to get ticket: %w", err)
		}
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

// SendImmediate attempts a direct stream send (no queue), falling back to enqueue.
func (c *Controller) SendImmediate(payload []byte) error {
	return c.Session.SendImmediate(payload)
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
