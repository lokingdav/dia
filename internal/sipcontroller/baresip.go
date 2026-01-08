package sipcontroller

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// BaresipEventType represents Baresip call event types
type BaresipEventType string

const (
	EventCallIncoming    BaresipEventType = "CALL_INCOMING"
	EventCallOutgoing    BaresipEventType = "CALL_OUTGOING"
	EventCallRinging     BaresipEventType = "CALL_RINGING"
	EventCallProgress    BaresipEventType = "CALL_PROGRESS"
	EventCallAnswered    BaresipEventType = "CALL_ANSWERED"
	EventCallEstablished BaresipEventType = "CALL_ESTABLISHED"
	EventCallClosed      BaresipEventType = "CALL_CLOSED"
	EventCallTransfer    BaresipEventType = "CALL_TRANSFER"
	EventCallDTMFStart   BaresipEventType = "CALL_DTMF_START"
	EventCallDTMFEnd     BaresipEventType = "CALL_DTMF_END"
	EventRegisterOK      BaresipEventType = "REGISTER_OK"
	EventRegisterFail    BaresipEventType = "REGISTER_FAIL"
	EventUnregistering   BaresipEventType = "UNREGISTERING"
)

// BaresipEvent represents an event from Baresip
type BaresipEvent struct {
	Event      bool             `json:"event"`
	Class      string           `json:"class"`
	Type       BaresipEventType `json:"type"`
	AccountAOR string           `json:"accountaor"`
	Direction  string           `json:"direction"`
	PeerURI    string           `json:"peeruri"`
	PeerName   string           `json:"peername"`
	ID         string           `json:"id"`
	Param      string           `json:"param"`
}

// BaresipResponse represents a command response from Baresip
type BaresipResponse struct {
	Response bool   `json:"response"`
	OK       bool   `json:"ok"`
	Data     string `json:"data"`
	Token    string `json:"token"`
}

// BaresipCommand represents a command to send to Baresip
type BaresipCommand struct {
	Command string `json:"command"`
	Params  string `json:"params,omitempty"`
	Token   string `json:"token,omitempty"`
}

// BaresipClient manages connection to Baresip ctrl_tcp
type BaresipClient struct {
	addr    string
	conn    net.Conn
	encoder *NetstringEncoder
	decoder *NetstringDecoder
	writeMu sync.Mutex

	eventChan    chan BaresipEvent
	responseChan chan BaresipResponse
	errorChan    chan error

	tokenCounter atomic.Uint64
	pendingCmds  map[string]chan BaresipResponse
	pendingMu    sync.Mutex
	cmdTimeout   time.Duration

	closed   atomic.Bool
	closedCh chan struct{}

	verbose bool
}

// NewBaresipClient creates a new Baresip client
func NewBaresipClient(addr string, verbose bool) *BaresipClient {
	return &BaresipClient{
		addr:         addr,
		eventChan:    make(chan BaresipEvent, 100),
		responseChan: make(chan BaresipResponse, 10),
		errorChan:    make(chan error, 1),
		pendingCmds:  make(map[string]chan BaresipResponse),
		closedCh:     make(chan struct{}),
		verbose:      verbose,
		cmdTimeout:   2 * time.Second,
	}
}

// Connect establishes connection to Baresip
func (b *BaresipClient) Connect() error {
	conn, err := net.Dial("tcp", b.addr)
	if err != nil {
		return fmt.Errorf("connecting to baresip at %s: %w", b.addr, err)
	}

	b.conn = conn
	b.encoder = NewNetstringEncoder(conn)
	b.decoder = NewNetstringDecoder(conn)

	// Start reader goroutine
	go b.readLoop()

	log.Printf("[Baresip] Connected to %s", b.addr)
	return nil
}

// Close closes the connection
func (b *BaresipClient) Close() error {
	if b.closed.Swap(true) {
		return nil // Already closed
	}
	close(b.closedCh)
	if b.conn != nil {
		return b.conn.Close()
	}
	return nil
}

// Events returns the channel for receiving Baresip events
func (b *BaresipClient) Events() <-chan BaresipEvent {
	return b.eventChan
}

// Errors returns the channel for receiving errors
func (b *BaresipClient) Errors() <-chan error {
	return b.errorChan
}

// readLoop continuously reads from Baresip and dispatches events/responses
func (b *BaresipClient) readLoop() {
	defer close(b.eventChan)
	defer close(b.errorChan)

	for {
		select {
		case <-b.closedCh:
			return
		default:
		}

		data, err := b.decoder.Decode()
		if err != nil {
			if !b.closed.Load() {
				b.errorChan <- fmt.Errorf("reading from baresip: %w", err)
			}
			return
		}

		if b.verbose {
			log.Printf("[Baresip] Received: %s", string(data))
		}

		// Parse JSON
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			log.Printf("[Baresip] Invalid JSON: %v", err)
			continue
		}

		// Determine if it's an event or response
		if _, isEvent := raw["event"]; isEvent {
			var event BaresipEvent
			if err := json.Unmarshal(data, &event); err != nil {
				log.Printf("[Baresip] Failed to parse event: %v", err)
				continue
			}
			select {
			case b.eventChan <- event:
			default:
				log.Printf("[Baresip] Event channel full, dropping event")
			}
		} else if _, isResponse := raw["response"]; isResponse {
			var resp BaresipResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				log.Printf("[Baresip] Failed to parse response: %v", err)
				continue
			}

			// Route to pending command if token matches
			if resp.Token != "" {
				b.pendingMu.Lock()
				if ch, ok := b.pendingCmds[resp.Token]; ok {
					ch <- resp
					delete(b.pendingCmds, resp.Token)
				}
				b.pendingMu.Unlock()
			} else {
				select {
				case b.responseChan <- resp:
				default:
				}
			}
		}
	}
}

// sendCommandCore sends a command, returning the time at which the netstring was written.
func (b *BaresipClient) sendCommandCore(cmd, params string) (*BaresipResponse, time.Time, error) {
	token := fmt.Sprintf("tok%d", b.tokenCounter.Add(1))

	command := BaresipCommand{
		Command: cmd,
		Params:  params,
		Token:   token,
	}

	data, err := json.Marshal(command)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("marshaling command: %w", err)
	}

	// Register pending command
	respChan := make(chan BaresipResponse, 1)
	b.pendingMu.Lock()
	b.pendingCmds[token] = respChan
	b.pendingMu.Unlock()

	if b.verbose {
		log.Printf("[Baresip] Sending: %s", string(data))
	}

	b.writeMu.Lock()
	sentAt := time.Now()
	err = b.encoder.Encode(data)
	b.writeMu.Unlock()
	if err != nil {
		b.pendingMu.Lock()
		delete(b.pendingCmds, token)
		b.pendingMu.Unlock()
		return nil, time.Time{}, fmt.Errorf("sending command: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respChan:
		return &resp, sentAt, nil
	case <-b.closedCh:
		b.pendingMu.Lock()
		delete(b.pendingCmds, token)
		b.pendingMu.Unlock()
		return nil, time.Time{}, fmt.Errorf("connection closed")
	case <-time.After(b.cmdTimeout):
		b.pendingMu.Lock()
		delete(b.pendingCmds, token)
		b.pendingMu.Unlock()
		return nil, time.Time{}, fmt.Errorf("command timeout: %s", cmd)
	}
}

// sendCommand sends a command and waits for response.
func (b *BaresipClient) sendCommand(cmd, params string) (*BaresipResponse, error) {
	resp, _, err := b.sendCommandCore(cmd, params)
	return resp, err
}

// sendCommandWithSentAt sends a command and returns the netstring write timestamp.
func (b *BaresipClient) sendCommandWithSentAt(cmd, params string) (*BaresipResponse, time.Time, error) {
	return b.sendCommandCore(cmd, params)
}

// Dial initiates an outgoing call
func (b *BaresipClient) Dial(uri string) (*BaresipResponse, error) {
	return b.sendCommand("dial", uri)
}

// DialWithSentAt initiates an outgoing call and returns the timestamp at which the dial command was written.
func (b *BaresipClient) DialWithSentAt(uri string) (*BaresipResponse, time.Time, error) {
	return b.sendCommandWithSentAt("dial", uri)
}

// Accept answers an incoming call
func (b *BaresipClient) Accept(callID string) (*BaresipResponse, error) {
	if callID != "" {
		return b.sendCommand("accept", callID)
	}
	return b.sendCommand("accept", "")
}

// Hangup terminates a call
func (b *BaresipClient) Hangup(callID string, scode int, reason string) (*BaresipResponse, error) {
	params := ""
	if callID != "" {
		params = callID
	}
	if scode > 0 {
		if params != "" {
			params += " "
		}
		params += fmt.Sprintf("scode=%d", scode)
	}
	if reason != "" {
		if params != "" {
			params += " "
		}
		params += fmt.Sprintf("reason=%s", reason)
	}
	return b.sendCommand("hangup", params)
}

// HangupAll hangs up all calls
func (b *BaresipClient) HangupAll() (*BaresipResponse, error) {
	return b.sendCommand("hangupall", "")
}

// ListCalls lists active calls
func (b *BaresipClient) ListCalls() (*BaresipResponse, error) {
	return b.sendCommand("listcalls", "")
}

// RegInfo gets registration info
func (b *BaresipClient) RegInfo() (*BaresipResponse, error) {
	return b.sendCommand("reginfo", "")
}
