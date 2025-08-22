package subscriber

// Controller is a small wrapper combining Client + Session for convenience.
type Controller struct {
	Client  *Client
	Session *Session
}

// NewController builds a Client and Session in one step.
func NewController(host string, port int, useTLS bool, topic string, ticket []byte, senderID string) (*Controller, error) {
	c, err := NewClient(host, port, useTLS)
	if err != nil {
		return nil, err
	}
	sess := NewSession(c, topic, ticket, senderID)
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
