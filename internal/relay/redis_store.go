package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// storedMsg is internal-only; not exposed in the public proto.
type storedMsg struct {
	Topic    string `json:"topic"`
	Payload  []byte `json:"payload"`
	SenderID string `json:"sender_id"`
}

// RedisStore handles message storage and retrieval using Redis
type RedisStore struct {
	client     *redis.Client
	messageTTL time.Duration
}

// NewRedisStore creates a new Redis-backed message store
func NewRedisStore(cfg *Config) (*RedisStore, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Printf("Connected to Redis at %s", cfg.RedisAddr)

	return &RedisStore{
		client:     rdb,
		messageTTL: time.Duration(cfg.MessageTTL) * time.Second,
	}, nil
}

// Close closes the Redis connection
func (rs *RedisStore) Close() error {
	return rs.client.Close()
}

// StoreMessage stores a message in Redis with TTL
func (rs *RedisStore) StoreMessage(ctx context.Context, topic string, payload []byte, senderID string) error {
	msg := storedMsg{
		Topic:    topic,
		Payload:  payload,
		SenderID: senderID,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	listKey := fmt.Sprintf("topic:%s:messages", topic)

	// Add message to the list (newest at head)
	if err := rs.client.LPush(ctx, listKey, data).Err(); err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	// Set/refresh TTL
	if err := rs.client.Expire(ctx, listKey, rs.messageTTL).Err(); err != nil {
		log.Printf("Warning: failed to set TTL on topic %s: %v", topic, err)
	}

	// Trim list to last 100 messages
	if err := rs.client.LTrim(ctx, listKey, 0, 99).Err(); err != nil {
		log.Printf("Warning: failed to trim topic %s: %v", topic, err)
	}

	return nil
}

// GetMessageHistory retrieves message history for a topic (oldest → newest)
func (rs *RedisStore) GetMessageHistory(ctx context.Context, topic string) ([]storedMsg, error) {
	listKey := fmt.Sprintf("topic:%s:messages", topic)

	data, err := rs.client.LRange(ctx, listKey, 0, -1).Result()
	if err != nil {
		if err == redis.Nil {
			return []storedMsg{}, nil // No messages found
		}
		return nil, fmt.Errorf("failed to get message history: %w", err)
	}

	msgs := make([]storedMsg, 0, len(data))
	for i := len(data) - 1; i >= 0; i-- { // LPUSH => reverse to deliver oldest→newest
		var m storedMsg
		if err := json.Unmarshal([]byte(data[i]), &m); err != nil {
			log.Printf("Warning: failed to unmarshal message: %v", err)
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// TopicExists checks if a topic exists (has any messages or is tracked)
func (rs *RedisStore) TopicExists(ctx context.Context, topic string) (bool, error) {
	listKey := fmt.Sprintf("topic:%s:messages", topic)
	topicKey := fmt.Sprintf("topic:%s:exists", topic)

	exists1, err := rs.client.Exists(ctx, listKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check topic existence: %w", err)
	}

	exists2, err := rs.client.Exists(ctx, topicKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check topic tracking: %w", err)
	}

	return exists1 > 0 || exists2 > 0, nil
}

// CreateTopic explicitly creates/tracks a topic
func (rs *RedisStore) CreateTopic(ctx context.Context, topic string) error {
	topicKey := fmt.Sprintf("topic:%s:exists", topic)
	if err := rs.client.Set(ctx, topicKey, "1", rs.messageTTL).Err(); err != nil {
		return fmt.Errorf("failed to create topic: %w", err)
	}
	return nil
}

// CleanupExpiredTopics: TTL cleanup is handled by Redis; no-op for now.
func (rs *RedisStore) CleanupExpiredTopics(ctx context.Context) error { return nil }

// GetTopicStats returns statistics about a topic
func (rs *RedisStore) GetTopicStats(ctx context.Context, topic string) (int64, error) {
	listKey := fmt.Sprintf("topic:%s:messages", topic)
	count, err := rs.client.LLen(ctx, listKey).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get topic stats: %w", err)
	}
	return count, nil
}
