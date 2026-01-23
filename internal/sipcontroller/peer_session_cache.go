package sipcontroller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type peerSessionCache struct {
	enabled bool
	client  *redis.Client
	prefix  string
	ttl     time.Duration
}

func newPeerSessionCache(cfg *Config) (*peerSessionCache, error) {
	if cfg == nil {
		return &peerSessionCache{enabled: false}, nil
	}
	if !cfg.CacheEnabled {
		return &peerSessionCache{enabled: false}, nil
	}
	addr := strings.TrimSpace(cfg.RedisAddr)
	if addr == "" {
		return nil, fmt.Errorf("--redis is required when --cache is enabled")
	}
	prefix := strings.TrimSpace(cfg.RedisPrefix)
	if prefix == "" {
		prefix = "denseid:dia:peer_session:v1"
	}
	var ttl time.Duration
	if cfg.PeerSessionTTLSeconds > 0 {
		ttl = time.Duration(cfg.PeerSessionTTLSeconds) * time.Second
	}

	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		Username: strings.TrimSpace(cfg.RedisUser),
		Password: cfg.RedisPass,
		DB:       cfg.RedisDB,
	})

	// Validate connectivity early for experiments.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &peerSessionCache{
		enabled: true,
		client:  c,
		prefix:  prefix,
		ttl:     ttl,
	}, nil
}

func (c *peerSessionCache) Close() {
	if c == nil || c.client == nil {
		return
	}
	_ = c.client.Close()
}

func (c *peerSessionCache) key(selfPhone, peerPhone string) string {
	selfPhone = strings.TrimSpace(selfPhone)
	peerPhone = strings.TrimSpace(peerPhone)
	return fmt.Sprintf("%s:%s:%s", c.prefix, selfPhone, peerPhone)
}

func (c *peerSessionCache) Get(ctx context.Context, selfPhone, peerPhone string) ([]byte, bool, error) {
	if c == nil || !c.enabled {
		return nil, false, nil
	}
	k := c.key(selfPhone, peerPhone)
	b, err := c.client.Get(ctx, k).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(b) == 0 {
		return nil, false, nil
	}
	return b, true, nil
}

func (c *peerSessionCache) Set(ctx context.Context, selfPhone, peerPhone string, blob []byte) error {
	if c == nil || !c.enabled {
		return nil
	}
	if len(blob) == 0 {
		return fmt.Errorf("empty peer session blob")
	}
	k := c.key(selfPhone, peerPhone)
	return c.client.Set(ctx, k, blob, c.ttl).Err()
}

func (c *peerSessionCache) ClearAll(ctx context.Context) (int64, error) {
	if c == nil || !c.enabled {
		return 0, nil
	}
	pattern := strings.TrimSpace(c.prefix) + ":*"
	var cursor uint64
	var deleted int64
	for {
		keys, next, err := c.client.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			return deleted, err
		}
		if len(keys) > 0 {
			n, err := c.client.Del(ctx, keys...).Result()
			if err != nil {
				return deleted, err
			}
			deleted += n
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return deleted, nil
}

// ClearPeerSessionCache clears all peer-session keys under the configured prefix.
// This is intended for scripting and testing.
func ClearPeerSessionCache(ctx context.Context, cfg *Config) (int64, error) {
	cache, err := newPeerSessionCache(cfg)
	if err != nil {
		return 0, err
	}
	defer cache.Close()
	return cache.ClearAll(ctx)
}
