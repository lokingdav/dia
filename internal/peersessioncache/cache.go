package peersessioncache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Options struct {
	Enabled  bool
	Addr     string
	Username string
	Password string
	DB       int
	Prefix   string
	TTL      time.Duration
}

type Cache struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func New(ctx context.Context, opts Options) (*Cache, error) {
	if !opts.Enabled {
		return nil, nil
	}
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		return nil, fmt.Errorf("redis addr is required when cache is enabled")
	}
	prefix := strings.TrimSpace(opts.Prefix)
	if prefix == "" {
		prefix = "denseid:dia:peer_session:v1"
	}

	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		Username: strings.TrimSpace(opts.Username),
		Password: opts.Password,
		DB:       opts.DB,
	})

	if ctx == nil {
		ctx = context.Background()
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := c.Ping(pingCtx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &Cache{client: c, prefix: prefix, ttl: opts.TTL}, nil
}

func (c *Cache) Close() {
	if c == nil || c.client == nil {
		return
	}
	_ = c.client.Close()
}

func (c *Cache) key(selfPhone, peerPhone string) string {
	selfPhone = strings.TrimSpace(selfPhone)
	peerPhone = strings.TrimSpace(peerPhone)
	return fmt.Sprintf("%s:%s:%s", c.prefix, selfPhone, peerPhone)
}

func (c *Cache) Get(ctx context.Context, selfPhone, peerPhone string) ([]byte, bool, error) {
	if c == nil || c.client == nil {
		return nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b, err := c.client.Get(ctx, c.key(selfPhone, peerPhone)).Bytes()
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

func (c *Cache) Set(ctx context.Context, selfPhone, peerPhone string, blob []byte) error {
	if c == nil || c.client == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(blob) == 0 {
		return fmt.Errorf("empty peer session blob")
	}
	return c.client.Set(ctx, c.key(selfPhone, peerPhone), blob, c.ttl).Err()
}
