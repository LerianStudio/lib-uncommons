// Package ratelimit provides rate limiting utilities including Redis-backed storage.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	libRedis "github.com/LerianStudio/lib-uncommons/uncommons/redis"
)

const (
	keyPrefix     = "ratelimit:"
	scanBatchSize = 100
)

// RedisStorage implements fiber.Storage interface using lib-uncommons Redis connection.
// This enables distributed rate limiting across multiple application instances.
type RedisStorage struct {
	conn *libRedis.RedisConnection
}

// NewRedisStorage creates a new Redis-backed storage for Fiber rate limiting.
// Returns nil if the Redis connection is nil.
func NewRedisStorage(conn *libRedis.RedisConnection) *RedisStorage {
	if conn == nil {
		return nil
	}

	return &RedisStorage{conn: conn}
}

// Get retrieves the value for the given key.
// Returns nil, nil when the key does not exist.
func (storage *RedisStorage) Get(key string) ([]byte, error) {
	if storage == nil || storage.conn == nil {
		return nil, nil
	}

	ctx := context.Background()

	client, err := storage.conn.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("get redis client: %w", err)
	}

	val, err := client.Get(ctx, keyPrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}

	return val, nil
}

// Set stores the given value for the given key with an expiration.
// 0 expiration means no expiration. Empty key or value will be ignored.
func (storage *RedisStorage) Set(key string, val []byte, exp time.Duration) error {
	if storage == nil || storage.conn == nil {
		return nil
	}

	if key == "" || len(val) == 0 {
		return nil
	}

	ctx := context.Background()

	client, err := storage.conn.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("get redis client: %w", err)
	}

	if err := client.Set(ctx, keyPrefix+key, val, exp).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}

	return nil
}

// Delete removes the value for the given key.
// Returns no error if the key does not exist.
func (storage *RedisStorage) Delete(key string) error {
	if storage == nil || storage.conn == nil {
		return nil
	}

	ctx := context.Background()

	client, err := storage.conn.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("get redis client: %w", err)
	}

	if err := client.Del(ctx, keyPrefix+key).Err(); err != nil {
		return fmt.Errorf("redis delete: %w", err)
	}

	return nil
}

// Reset clears all rate limit keys from the storage.
// This uses SCAN to find and delete keys with the rate limit prefix.
func (storage *RedisStorage) Reset() error {
	if storage == nil || storage.conn == nil {
		return nil
	}

	ctx := context.Background()

	client, err := storage.conn.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("get redis client: %w", err)
	}

	var cursor uint64

	for {
		keys, nextCursor, err := client.Scan(ctx, cursor, keyPrefix+"*", scanBatchSize).Result()
		if err != nil {
			return fmt.Errorf("redis scan: %w", err)
		}

		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("redis batch delete: %w", err)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

// Close is a no-op as the Redis connection is managed by the application lifecycle.
func (*RedisStorage) Close() error {
	return nil
}
