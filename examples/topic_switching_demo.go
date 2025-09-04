package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dense-identity/denseid/internal/subscriber"
)

// Protocol states for coordination
type ProtocolState int

const (
	StateIdle ProtocolState = iota
	StateWaitingForHandshake
	StateWaitingForTopicSwitch
	StateWaitingForFinal
	StateDone
)

// Client represents a protocol participant
type Client struct {
	name      string
	session   *subscriber.StreamSession
	state     ProtocolState
	mu        sync.Mutex
	responses chan string
}

func NewClient(name, address string) (*Client, error) {
	client, err := subscriber.NewRelayClient(address, false)
	if err != nil {
		return nil, err
	}

	c := &Client{
		name:      name,
		responses: make(chan string, 10),
		state:     StateIdle,
	}

	c.session = subscriber.NewStreamSession(client, name)
	if err := c.session.Start(c.handleMessage); err != nil {
		client.Close()
		return nil, err
	}

	return c, nil
}

func (c *Client) handleMessage(payload []byte) {
	message := string(payload)
	fmt.Printf("%s received: %s\n", c.name, message)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Send to response channel for protocol coordination
	select {
	case c.responses <- message:
	default:
		// Channel full, message dropped
	}
}

func (c *Client) sendAndWaitForResponse(message string, timeout time.Duration) (string, error) {
	// Clear any old responses
	c.mu.Lock()
	for len(c.responses) > 0 {
		<-c.responses
	}
	c.mu.Unlock()

	// Send message
	if err := c.session.Publish([]byte(message)); err != nil {
		return "", fmt.Errorf("failed to send message: %v", err)
	}

	// Wait for response
	select {
	case response := <-c.responses:
		return response, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for response")
	}
}

func (c *Client) waitForRequest(timeout time.Duration) (string, error) {
	select {
	case request := <-c.responses:
		return request, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for request")
	}
}

func (c *Client) respond(message string) error {
	return c.session.Publish([]byte(message))
}

func (c *Client) subscribeTo(topic string) error {
	return c.session.Subscribe(topic, nil, false)
}

func (c *Client) switchTo(topic string) error {
	return c.session.SwitchTopic(topic, nil, false)
}

func (c *Client) Close() {
	c.session.Close()
}

func main() {
	fmt.Println("=== DenseID Topic Switching Protocol Demo ===")

	// Create Alice and Bob clients
	fmt.Println("Creating Alice...")
	alice, err := NewClient("alice", "localhost:50054")
	if err != nil {
		log.Fatalf("Failed to create Alice: %v", err)
	}
	defer alice.Close()

	fmt.Println("Creating Bob...")
	bob, err := NewClient("bob", "localhost:50054")
	if err != nil {
		log.Fatalf("Failed to create Bob: %v", err)
	}
	defer bob.Close()

	// Phase 1: Initial handshake protocol
	fmt.Println("\n=== Phase 1: Handshake Protocol ===")

	topic1 := "handshake_topic_12345"

	// Both subscribe to handshake topic
	if err := alice.subscribeTo(topic1); err != nil {
		log.Fatalf("Alice failed to subscribe: %v", err)
	}
	if err := bob.subscribeTo(topic1); err != nil {
		log.Fatalf("Bob failed to subscribe: %v", err)
	}

	// Start Bob's response handler
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		request, err := bob.waitForRequest(10 * time.Second)
		if err != nil {
			fmt.Printf("Bob missed handshake: %v\n", err)
			return
		}
		if request == "HANDSHAKE_REQUEST:alice" {
			fmt.Println("Bob received handshake request, responding...")
			bob.respond("HANDSHAKE_ACK:bob")
		}
	}()

	// Alice initiates handshake (after Bob is listening)
	fmt.Println("Alice initiating handshake...")
	response, err := alice.sendAndWaitForResponse("HANDSHAKE_REQUEST:alice", 10*time.Second)
	if err != nil {
		log.Fatalf("Alice handshake failed: %v", err)
	}

	// Verify Alice got Bob's response
	if response != "HANDSHAKE_ACK:bob" {
		log.Fatalf("Unexpected handshake response: %s", response)
	}
	fmt.Println("✅ Handshake successful!")
	wg.Wait()

	// Phase 2: Coordinated topic switch
	fmt.Println("\n=== Phase 2: Coordinated Topic Switch ===")

	topic2 := "secure_topic_67890"

	// Start Bob's topic switch handler
	wg.Add(1)
	go func() {
		defer wg.Done()
		request, err := bob.waitForRequest(10 * time.Second)
		if err != nil {
			fmt.Printf("Bob missed topic switch proposal: %v\n", err)
			return
		}
		if request == "SWITCH_TOPIC:"+topic2 {
			fmt.Println("Bob accepting topic switch...")
			// Bob responds first, THEN switches
			bob.respond("SWITCH_ACK:" + topic2)
			// Then Bob switches
			if err := bob.switchTo(topic2); err != nil {
				fmt.Printf("Bob failed to switch topics: %v\n", err)
				return
			}
		}
	}()

	// Alice proposes topic switch
	fmt.Println("Alice proposing topic switch...")
	response, err = alice.sendAndWaitForResponse("SWITCH_TOPIC:"+topic2, 10*time.Second)
	if err != nil {
		log.Fatalf("Topic switch proposal failed: %v", err)
	}

	// Verify Bob accepted and Alice switches
	if response != "SWITCH_ACK:"+topic2 {
		log.Fatalf("Unexpected switch response: %s", response)
	}

	fmt.Println("Alice switching to new topic...")
	if err := alice.switchTo(topic2); err != nil {
		log.Fatalf("Alice failed to switch topics: %v", err)
	}

	fmt.Println("✅ Topic switch coordinated successfully!")
	wg.Wait()

	// Phase 3: Continue protocol on new topic
	fmt.Println("\n=== Phase 3: Protocol on New Topic ===")

	// Start Bob's secure message handler
	wg.Add(1)
	go func() {
		defer wg.Done()
		request, err := bob.waitForRequest(10 * time.Second)
		if err != nil {
			fmt.Printf("Bob missed secure message: %v\n", err)
			return
		}
		if request == "SECURE_MSG:Hello on secure channel" {
			fmt.Println("Bob processing secure message...")
			bob.respond("SECURE_ACK:Message processed securely")
		}
	}()

	// Alice sends secure message
	response, err = alice.sendAndWaitForResponse("SECURE_MSG:Hello on secure channel", 10*time.Second)
	if err != nil {
		log.Fatalf("Secure message failed: %v", err)
	}

	if response != "SECURE_ACK:Message processed securely" {
		log.Fatalf("Unexpected secure response: %s", response)
	}

	fmt.Println("✅ Secure communication successful!")
	wg.Wait()

	fmt.Println("\n=== Protocol Completed Successfully ===")
	fmt.Println("✅ Request-response coordination working")
	fmt.Println("✅ Seamless topic switching without connection drops")
	fmt.Println("✅ Both parties coordinated properly")
}
