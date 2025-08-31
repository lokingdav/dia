package subscriber

import "github.com/dense-identity/denseid/internal/protocol"

// Controller is a small wrapper combining Client + Session for convenience.
type Controller struct {
	Client  *RelayClient
	Session *Session
}

// NewController builds a Client and Session in one step.
func NewController(callState *protocol.CallState) (*Controller, error) {
	c, err := NewRelayClient(callState.Config.RelayServerAddr, callState.Config.UseTls)
	if err != nil {
		return nil, err
	}
	sess := NewSession(c, callState.Topic, callState.Ticket, callState.SenderId)
	return &Controller{
		Client:  c,
		Session: sess,
	}, nil
}

// Start begins receiving; provide a function to handle inbound payload bytes.
func (c *Controller) Start(onMessage func([]byte)) {
	c.Session.Start(onMessage)
}

// Send publishes one payload (unauthenticated publish as per server policy).
func (c *Controller) Send(payload []byte) error {
	return c.Session.Send(payload)
}

// Close shuts down the session and the underlying client connection.
func (c *Controller) Close() error {
	c.Session.Close()
	return c.Client.Close()
}
