//go:build unit

package ratelimit

import (
	"testing"
	"time"

	libLog "github.com/LerianStudio/lib-uncommons/uncommons/log"
	libRedis "github.com/LerianStudio/lib-uncommons/uncommons/redis"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedisConnection(mr *miniredis.Miniredis) *libRedis.RedisConnection {
	return &libRedis.RedisConnection{
		Address: []string{mr.Addr()},
		Logger:  &libLog.NoneLogger{},
	}
}

func TestNewRedisStorage_NilConnection(t *testing.T) {
	t.Parallel()

	storage := NewRedisStorage(nil)
	assert.Nil(t, storage)
}

func TestNewRedisStorage_ValidConnection(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	conn := newTestRedisConnection(mr)

	storage := NewRedisStorage(conn)
	require.NotNil(t, storage)
	require.NotNil(t, storage.conn)
}

func TestRedisStorage_GetSetDelete(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	conn := newTestRedisConnection(mr)
	require.NoError(t, conn.Connect(t.Context()))
	t.Cleanup(func() { _ = conn.Close() })

	storage := NewRedisStorage(conn)
	require.NotNil(t, storage)

	key := "test-key"
	value := []byte("test-value")

	val, err := storage.Get(key)
	require.NoError(t, err)
	assert.Nil(t, val)

	err = storage.Set(key, value, time.Minute)
	require.NoError(t, err)

	val, err = storage.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, val)

	err = storage.Delete(key)
	require.NoError(t, err)

	val, err = storage.Get(key)
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestRedisStorage_SetEmptyKeyIgnored(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	conn := newTestRedisConnection(mr)
	require.NoError(t, conn.Connect(t.Context()))
	t.Cleanup(func() { _ = conn.Close() })

	storage := NewRedisStorage(conn)
	require.NotNil(t, storage)

	err := storage.Set("", []byte("value"), time.Minute)
	require.NoError(t, err)

	err = storage.Set("key", nil, time.Minute)
	require.NoError(t, err)
}

func TestRedisStorage_Reset(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	conn := newTestRedisConnection(mr)
	require.NoError(t, conn.Connect(t.Context()))
	t.Cleanup(func() { _ = conn.Close() })

	storage := NewRedisStorage(conn)
	require.NotNil(t, storage)

	require.NoError(t, storage.Set("key1", []byte("val1"), time.Minute))
	require.NoError(t, storage.Set("key2", []byte("val2"), time.Minute))

	err := storage.Reset()
	require.NoError(t, err)

	val, err := storage.Get("key1")
	require.NoError(t, err)
	assert.Nil(t, val)

	val, err = storage.Get("key2")
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestRedisStorage_Close(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	conn := newTestRedisConnection(mr)

	storage := NewRedisStorage(conn)
	require.NotNil(t, storage)

	err := storage.Close()
	require.NoError(t, err)
}

func TestRedisStorage_NilStorageOperations(t *testing.T) {
	t.Parallel()

	var storage *RedisStorage

	val, err := storage.Get("key")
	require.NoError(t, err)
	assert.Nil(t, val)

	err = storage.Set("key", []byte("value"), time.Minute)
	require.NoError(t, err)

	err = storage.Delete("key")
	require.NoError(t, err)

	err = storage.Reset()
	require.NoError(t, err)

	err = storage.Close()
	require.NoError(t, err)
}

func TestRedisStorage_KeyPrefix(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	conn := newTestRedisConnection(mr)
	require.NoError(t, conn.Connect(t.Context()))
	t.Cleanup(func() { _ = conn.Close() })

	storage := NewRedisStorage(conn)
	require.NotNil(t, storage)

	require.NoError(t, storage.Set("test", []byte("value"), time.Minute))

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	t.Cleanup(func() { _ = client.Close() })

	val, err := client.Get(t.Context(), "ratelimit:test").Bytes()
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}
