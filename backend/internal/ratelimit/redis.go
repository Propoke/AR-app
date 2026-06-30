package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisStore adapts a Redis client to the Store interface.
type redisStore struct {
	rdb *redis.Client
}

// NewRedisStore returns a Store backed by Redis.
func NewRedisStore(rdb *redis.Client) Store {
	return &redisStore{rdb: rdb}
}

func (s *redisStore) Incr(ctx context.Context, key string) (int64, error) {
	return s.rdb.Incr(ctx, key).Result()
}

func (s *redisStore) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return s.rdb.Expire(ctx, key, ttl).Err()
}
