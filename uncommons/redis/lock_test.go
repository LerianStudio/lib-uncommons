package redis

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestClient(t *testing.T) *Client {
	t.Helper()

	mr := miniredis.RunT(t)

	client, err := New(context.Background(), Config{
		Topology: Topology{
			Standalone: &StandaloneTopology{Address: mr.Addr()},
		},
		Logger: &log.NopLogger{},
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, client.Close())
		mr.Close()
	})

	return client
}

// setupTestLock creates a Redis client and DistributedLock for testing.
func setupTestLock(t *testing.T) (*Client, *DistributedLock) {
	t.Helper()

	client := setupTestClient(t)

	lock, err := NewDistributedLock(client)
	require.NoError(t, err)

	return client, lock
}

func TestDistributedLock_WithLock(t *testing.T) {
	client := setupTestClient(t)

	lock, err := NewDistributedLock(client)
	require.NoError(t, err)

	executed := false
	err = lock.WithLock(context.Background(), "test:lock", func(context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestDistributedLock_WithLock_ErrorPropagation(t *testing.T) {
	client := setupTestClient(t)

	lock, err := NewDistributedLock(client)
	require.NoError(t, err)

	expectedErr := errors.New("boom")
	err = lock.WithLock(context.Background(), "test:lock", func(context.Context) error {
		return expectedErr
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestDistributedLock_ConcurrentExecutionSingleKey(t *testing.T) {
	client := setupTestClient(t)

	lock, err := NewDistributedLock(client)
	require.NoError(t, err)

	ctx := context.Background()
	var currentConcurrent int32
	var maxConcurrent int32
	var total int32

	opts := LockOptions{
		Expiry:      5 * time.Second,
		Tries:       50,
		RetryDelay:  20 * time.Millisecond,
		DriftFactor: 0.01,
	}

	const workers = 10
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)

	for range workers {
		go func() {
			defer wg.Done()

			err := lock.WithLockOptions(ctx, "test:concurrent", opts, func(context.Context) error {
				active := atomic.AddInt32(&currentConcurrent, 1)
				if active > atomic.LoadInt32(&maxConcurrent) {
					atomic.StoreInt32(&maxConcurrent, active)
				}

				atomic.AddInt32(&total, 1)
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&currentConcurrent, -1)

				return nil
			})
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, int32(workers), total)
	assert.Equal(t, int32(1), maxConcurrent)
}

func TestDistributedLock_TryLock_Contention(t *testing.T) {
	client := setupTestClient(t)

	lock, err := NewDistributedLock(client)
	require.NoError(t, err)

	ctx := context.Background()

	mutex1, acquired, err := lock.TryLock(ctx, "test:contention")
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, mutex1)
	defer func() {
		require.NoError(t, lock.Unlock(ctx, mutex1))
	}()

	mutex2, acquired, err := lock.TryLock(ctx, "test:contention")
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Nil(t, mutex2)
}

func TestDistributedLock_PanicRecovery(t *testing.T) {
	client := setupTestClient(t)

	lock, err := NewDistributedLock(client)
	require.NoError(t, err)

	ctx := context.Background()

	require.Panics(t, func() {
		_ = lock.WithLock(ctx, "test:panic", func(context.Context) error {
			panic("panic inside lock")
		})
	})

	executed := false
	err = lock.WithLock(ctx, "test:panic", func(context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestDistributedLock_NilAndInitGuards(t *testing.T) {
	t.Run("new lock with nil client", func(t *testing.T) {
		lock, err := NewDistributedLock(nil)
		require.Error(t, err)
		assert.Nil(t, lock)
	})

	t.Run("nil receiver", func(t *testing.T) {
		var dl *DistributedLock
		ctx := context.Background()

		err := dl.WithLock(ctx, "test:key", func(context.Context) error { return nil })
		assert.ErrorContains(t, err, "distributed lock is nil")

		err = dl.WithLockOptions(ctx, "test:key", DefaultLockOptions(), func(context.Context) error { return nil })
		assert.ErrorContains(t, err, "distributed lock is nil")

		mutex, acquired, err := dl.TryLock(ctx, "test:key")
		assert.ErrorContains(t, err, "distributed lock is nil")
		assert.Nil(t, mutex)
		assert.False(t, acquired)

		err = dl.Unlock(ctx, nil)
		assert.ErrorContains(t, err, "distributed lock is nil")
	})

	t.Run("zero value lock is rejected", func(t *testing.T) {
		dl := &DistributedLock{}
		ctx := context.Background()

		err := dl.WithLockOptions(ctx, "test:key", DefaultLockOptions(), func(context.Context) error { return nil })
		assert.ErrorContains(t, err, "distributed lock is not initialized")

		mutex, acquired, err := dl.TryLock(ctx, "test:key")
		assert.ErrorContains(t, err, "distributed lock is not initialized")
		assert.Nil(t, mutex)
		assert.False(t, acquired)
	})
}

func TestDistributedLock_OptionValidation(t *testing.T) {
	client := setupTestClient(t)

	lock, err := NewDistributedLock(client)
	require.NoError(t, err)

	err = lock.WithLockOptions(context.Background(), "", DefaultLockOptions(), func(context.Context) error { return nil })
	assert.ErrorContains(t, err, "lock key cannot be empty")

	err = lock.WithLockOptions(context.Background(), "test:key", DefaultLockOptions(), nil)
	assert.ErrorContains(t, err, "fn is nil")

	err = lock.WithLockOptions(context.Background(), "test:key", LockOptions{
		Expiry:      0,
		Tries:       1,
		RetryDelay:  time.Millisecond,
		DriftFactor: 0.01,
	}, func(context.Context) error { return nil })
	assert.ErrorContains(t, err, "lock expiry must be greater than 0")

	err = lock.WithLockOptions(context.Background(), "test:key", LockOptions{
		Expiry:      time.Second,
		Tries:       0,
		RetryDelay:  time.Millisecond,
		DriftFactor: 0.01,
	}, func(context.Context) error { return nil })
	assert.ErrorContains(t, err, "lock tries must be at least 1")

	err = lock.WithLockOptions(context.Background(), "test:key", LockOptions{
		Expiry:      time.Second,
		Tries:       1,
		RetryDelay:  -time.Millisecond,
		DriftFactor: 0.01,
	}, func(context.Context) error { return nil })
	assert.ErrorContains(t, err, "lock retry delay cannot be negative")

	err = lock.WithLockOptions(context.Background(), "test:key", LockOptions{
		Expiry:      time.Second,
		Tries:       1,
		RetryDelay:  time.Millisecond,
		DriftFactor: 1,
	}, func(context.Context) error { return nil })
	assert.ErrorContains(t, err, "lock drift factor")
}

func TestLockOptionFactories(t *testing.T) {
	defaultOpts := DefaultLockOptions()
	assert.Equal(t, 10*time.Second, defaultOpts.Expiry)
	assert.Equal(t, 3, defaultOpts.Tries)
	assert.Equal(t, 500*time.Millisecond, defaultOpts.RetryDelay)
	assert.Equal(t, 0.01, defaultOpts.DriftFactor)

	rateLimiterOpts := RateLimiterLockOptions()
	assert.Equal(t, 2*time.Second, rateLimiterOpts.Expiry)
	assert.Equal(t, 2, rateLimiterOpts.Tries)
	assert.Equal(t, 100*time.Millisecond, rateLimiterOpts.RetryDelay)
	assert.Equal(t, 0.01, rateLimiterOpts.DriftFactor)
}

func TestSafeLockKeyForLogs(t *testing.T) {
	safe := safeLockKeyForLogs("lock:tenant\n123")
	assert.NotContains(t, safe, "\n")
	assert.Contains(t, safe, "\\n")

	longKey := strings.Repeat("a", 1024)
	safeLong := safeLockKeyForLogs(longKey)
	assert.Contains(t, safeLong, "...(truncated)")
}

// --- New comprehensive test coverage below ---

func TestDistributedLock_WithLock_ContextPassedToFn(t *testing.T) {
	_, lock := setupTestLock(t)

	type ctxKey string
	key := ctxKey("trace-id")

	ctx := context.WithValue(context.Background(), key, "abc-123")

	err := lock.WithLock(ctx, "test:ctx-pass", func(ctx context.Context) error {
		val, ok := ctx.Value(key).(string)
		assert.True(t, ok)
		assert.Equal(t, "abc-123", val)

		return nil
	})

	require.NoError(t, err)
}

func TestDistributedLock_WithLockOptions_CustomExpiry(t *testing.T) {
	_, lock := setupTestLock(t)

	opts := LockOptions{
		Expiry:      30 * time.Second,
		Tries:       1,
		RetryDelay:  time.Millisecond,
		DriftFactor: 0.01,
	}

	executed := false

	err := lock.WithLockOptions(context.Background(), "test:custom-opts", opts, func(context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestDistributedLock_WithLock_WhitespaceOnlyKey(t *testing.T) {
	_, lock := setupTestLock(t)

	err := lock.WithLock(context.Background(), "   ", func(context.Context) error {
		return nil
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "lock key cannot be empty")
}

func TestDistributedLock_WithLock_TabAndNewlineKey(t *testing.T) {
	_, lock := setupTestLock(t)

	err := lock.WithLock(context.Background(), "\t\n", func(context.Context) error {
		return nil
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "lock key cannot be empty")
}

func TestDistributedLock_TryLock_EmptyKey(t *testing.T) {
	_, lock := setupTestLock(t)

	mutex, acquired, err := lock.TryLock(context.Background(), "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "lock key cannot be empty")
	assert.False(t, acquired)
	assert.Nil(t, mutex)
}

func TestDistributedLock_TryLock_WhitespaceOnlyKey(t *testing.T) {
	_, lock := setupTestLock(t)

	mutex, acquired, err := lock.TryLock(context.Background(), "   ")
	require.Error(t, err)
	assert.ErrorContains(t, err, "lock key cannot be empty")
	assert.False(t, acquired)
	assert.Nil(t, mutex)
}

func TestDistributedLock_TryLock_SuccessfulAcquireAndRelease(t *testing.T) {
	_, lock := setupTestLock(t)

	ctx := context.Background()

	mutex, acquired, err := lock.TryLock(ctx, "test:try-success")
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, mutex)

	// Release the lock
	err = lock.Unlock(ctx, mutex)
	require.NoError(t, err)

	// Lock should be available again
	mutex2, acquired2, err := lock.TryLock(ctx, "test:try-success")
	require.NoError(t, err)
	assert.True(t, acquired2)
	assert.NotNil(t, mutex2)

	// Clean up
	require.NoError(t, lock.Unlock(ctx, mutex2))
}

func TestDistributedLock_TryLock_DifferentKeysNoContention(t *testing.T) {
	_, lock := setupTestLock(t)

	ctx := context.Background()

	mutex1, acquired1, err := lock.TryLock(ctx, "test:key-a")
	require.NoError(t, err)
	require.True(t, acquired1)
	require.NotNil(t, mutex1)
	defer func() { _ = lock.Unlock(ctx, mutex1) }()

	// Different key should not contend
	mutex2, acquired2, err := lock.TryLock(ctx, "test:key-b")
	require.NoError(t, err)
	assert.True(t, acquired2)
	assert.NotNil(t, mutex2)
	defer func() { _ = lock.Unlock(ctx, mutex2) }()
}

func TestDistributedLock_Unlock_NilMutex(t *testing.T) {
	_, lock := setupTestLock(t)

	err := lock.Unlock(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "mutex is nil")
}

func TestDistributedLock_ConcurrentTryLock(t *testing.T) {
	_, lock := setupTestLock(t)

	ctx := context.Background()

	const workers = 20
	var acquired int32

	var wg sync.WaitGroup

	wg.Add(workers)

	for range workers {
		go func() {
			defer wg.Done()

			mutex, ok, err := lock.TryLock(ctx, "test:concurrent-try")
			if err != nil {
				return
			}

			if ok {
				atomic.AddInt32(&acquired, 1)
				// Hold lock briefly
				time.Sleep(10 * time.Millisecond)
				_ = lock.Unlock(ctx, mutex)
			}
		}()
	}

	wg.Wait()

	// At least one goroutine must have acquired the lock
	assert.GreaterOrEqual(t, atomic.LoadInt32(&acquired), int32(1))
}

func TestDistributedLock_ConcurrentDifferentKeys(t *testing.T) {
	_, lock := setupTestLock(t)

	ctx := context.Background()

	const workers = 5
	var wg sync.WaitGroup

	wg.Add(workers)

	errCh := make(chan error, workers)

	for i := range workers {
		go func(idx int) {
			defer wg.Done()

			key := "test:concurrent-diff:" + strings.Repeat("x", idx+1)

			err := lock.WithLock(ctx, key, func(context.Context) error {
				time.Sleep(5 * time.Millisecond)
				return nil
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestDistributedLock_WithLockOptions_NegativeDriftFactor(t *testing.T) {
	_, lock := setupTestLock(t)

	err := lock.WithLockOptions(context.Background(), "test:key", LockOptions{
		Expiry:      time.Second,
		Tries:       1,
		RetryDelay:  time.Millisecond,
		DriftFactor: -0.5,
	}, func(context.Context) error { return nil })

	require.Error(t, err)
	assert.ErrorContains(t, err, "lock drift factor")
}

func TestDistributedLock_WithLockOptions_NegativeExpiry(t *testing.T) {
	_, lock := setupTestLock(t)

	err := lock.WithLockOptions(context.Background(), "test:key", LockOptions{
		Expiry:      -time.Second,
		Tries:       1,
		RetryDelay:  time.Millisecond,
		DriftFactor: 0.01,
	}, func(context.Context) error { return nil })

	require.Error(t, err)
	assert.ErrorContains(t, err, "lock expiry must be greater than 0")
}

func TestDistributedLock_WithLockOptions_ZeroRetryDelay(t *testing.T) {
	_, lock := setupTestLock(t)

	// Zero retry delay is valid (no delay between retries)
	executed := false

	err := lock.WithLockOptions(context.Background(), "test:zero-delay", LockOptions{
		Expiry:      time.Second,
		Tries:       1,
		RetryDelay:  0,
		DriftFactor: 0.01,
	}, func(context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestDistributedLock_WithLockOptions_DriftFactorBoundary(t *testing.T) {
	_, lock := setupTestLock(t)

	// DriftFactor = 0 is valid (lower bound inclusive)
	executed := false

	err := lock.WithLockOptions(context.Background(), "test:drift-zero", LockOptions{
		Expiry:      time.Second,
		Tries:       1,
		RetryDelay:  time.Millisecond,
		DriftFactor: 0,
	}, func(context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)

	// DriftFactor = 0.99 is valid (just under 1)
	executed = false

	err = lock.WithLockOptions(context.Background(), "test:drift-high", LockOptions{
		Expiry:      time.Second,
		Tries:       1,
		RetryDelay:  time.Millisecond,
		DriftFactor: 0.99,
	}, func(context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestDistributedLock_WithLock_ContextionExhaustsRetries(t *testing.T) {
	_, lock := setupTestLock(t)

	ctx := context.Background()

	// Acquire lock and hold it
	mutex, acquired, err := lock.TryLock(ctx, "test:exhaust")
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = lock.Unlock(ctx, mutex) }()

	// Try to acquire the same key with limited retries - should fail
	err = lock.WithLockOptions(ctx, "test:exhaust", LockOptions{
		Expiry:      time.Second,
		Tries:       1,
		RetryDelay:  time.Millisecond,
		DriftFactor: 0.01,
	}, func(context.Context) error {
		t.Fatal("function should not be executed when lock cannot be acquired")
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire lock")
}

func TestDistributedLock_ContextCancellation(t *testing.T) {
	_, lock := setupTestLock(t)

	// Acquire lock to create contention
	bgCtx := context.Background()
	mutex, acquired, err := lock.TryLock(bgCtx, "test:cancel")
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = lock.Unlock(bgCtx, mutex) }()

	// Create a context that will be cancelled quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Try to acquire the same lock with the cancellable context
	err = lock.WithLockOptions(ctx, "test:cancel", LockOptions{
		Expiry:      5 * time.Second,
		Tries:       100,
		RetryDelay:  50 * time.Millisecond,
		DriftFactor: 0.01,
	}, func(context.Context) error {
		t.Fatal("function should not execute when context is cancelled")
		return nil
	})

	require.Error(t, err)
}

func TestDistributedLock_WithLock_FnReceivesSpanContext(t *testing.T) {
	// Verify the function receives a context (potentially enriched with span)
	_, lock := setupTestLock(t)

	err := lock.WithLock(context.Background(), "test:span-ctx", func(ctx context.Context) error {
		// The context should be non-nil and usable
		require.NotNil(t, ctx)

		return nil
	})

	require.NoError(t, err)
}

func TestSafeLockKeyForLogs_ShortKey(t *testing.T) {
	safe := safeLockKeyForLogs("lock:simple")
	// Short keys should be returned as-is (quoted)
	assert.NotContains(t, safe, "...(truncated)")
	assert.Contains(t, safe, "lock:simple")
}

func TestSafeLockKeyForLogs_ExactBoundary(t *testing.T) {
	// Key that produces a quoted string of exactly 128 characters
	// QuoteToASCII adds 2 quote characters, so we need 126 inner chars
	key := strings.Repeat("b", 126)
	safe := safeLockKeyForLogs(key)
	assert.NotContains(t, safe, "...(truncated)")
}

func TestSafeLockKeyForLogs_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "tab character",
			input:    "lock:key\twith\ttabs",
			contains: "\\t",
		},
		{
			name:     "null byte",
			input:    "lock:key\x00null",
			contains: "\\x00",
		},
		{
			name:     "unicode",
			input:    "lock:key:emoji:😀",
			contains: "lock:key:emoji:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe := safeLockKeyForLogs(tt.input)
			assert.Contains(t, safe, tt.contains)
		})
	}
}

func TestSafeLockKeyForLogs_EmptyKey(t *testing.T) {
	safe := safeLockKeyForLogs("")
	// QuoteToASCII on empty string returns `""`
	assert.Equal(t, `""`, safe)
}

func TestValidateLockOptions_AllInvalid(t *testing.T) {
	tests := []struct {
		name    string
		opts    LockOptions
		errText string
	}{
		{
			name: "zero expiry",
			opts: LockOptions{
				Expiry:      0,
				Tries:       1,
				RetryDelay:  time.Millisecond,
				DriftFactor: 0.01,
			},
			errText: "lock expiry must be greater than 0",
		},
		{
			name: "negative expiry",
			opts: LockOptions{
				Expiry:      -5 * time.Second,
				Tries:       1,
				RetryDelay:  time.Millisecond,
				DriftFactor: 0.01,
			},
			errText: "lock expiry must be greater than 0",
		},
		{
			name: "zero tries",
			opts: LockOptions{
				Expiry:      time.Second,
				Tries:       0,
				RetryDelay:  time.Millisecond,
				DriftFactor: 0.01,
			},
			errText: "lock tries must be at least 1",
		},
		{
			name: "negative tries",
			opts: LockOptions{
				Expiry:      time.Second,
				Tries:       -1,
				RetryDelay:  time.Millisecond,
				DriftFactor: 0.01,
			},
			errText: "lock tries must be at least 1",
		},
		{
			name: "negative retry delay",
			opts: LockOptions{
				Expiry:      time.Second,
				Tries:       1,
				RetryDelay:  -time.Millisecond,
				DriftFactor: 0.01,
			},
			errText: "lock retry delay cannot be negative",
		},
		{
			name: "drift factor equals 1",
			opts: LockOptions{
				Expiry:      time.Second,
				Tries:       1,
				RetryDelay:  time.Millisecond,
				DriftFactor: 1.0,
			},
			errText: "lock drift factor must be between 0",
		},
		{
			name: "drift factor greater than 1",
			opts: LockOptions{
				Expiry:      time.Second,
				Tries:       1,
				RetryDelay:  time.Millisecond,
				DriftFactor: 1.5,
			},
			errText: "lock drift factor must be between 0",
		},
		{
			name: "negative drift factor",
			opts: LockOptions{
				Expiry:      time.Second,
				Tries:       1,
				RetryDelay:  time.Millisecond,
				DriftFactor: -0.1,
			},
			errText: "lock drift factor must be between 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLockOptions(tt.opts)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.errText)
		})
	}
}

func TestValidateLockOptions_Valid(t *testing.T) {
	tests := []struct {
		name string
		opts LockOptions
	}{
		{
			name: "default options",
			opts: DefaultLockOptions(),
		},
		{
			name: "rate limiter options",
			opts: RateLimiterLockOptions(),
		},
		{
			name: "minimal valid",
			opts: LockOptions{
				Expiry:      time.Millisecond,
				Tries:       1,
				RetryDelay:  0,
				DriftFactor: 0,
			},
		},
		{
			name: "large values",
			opts: LockOptions{
				Expiry:      time.Hour,
				Tries:       1000,
				RetryDelay:  time.Minute,
				DriftFactor: 0.99,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLockOptions(tt.opts)
			require.NoError(t, err)
		})
	}
}

func TestDistributedLock_WithLock_MultipleSequentialLocks(t *testing.T) {
	_, lock := setupTestLock(t)

	ctx := context.Background()
	var order []int

	for i := range 5 {
		idx := i

		err := lock.WithLock(ctx, "test:sequential", func(context.Context) error {
			order = append(order, idx)
			return nil
		})
		require.NoError(t, err)
	}

	assert.Len(t, order, 5)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, order)
}

func TestDistributedLock_WithLock_LongLockKey(t *testing.T) {
	_, lock := setupTestLock(t)

	// Redis supports keys up to 512MB; test with a reasonably long key
	longKey := "test:" + strings.Repeat("x", 500)
	executed := false

	err := lock.WithLock(context.Background(), longKey, func(context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestDistributedLock_InterfaceCompliance(t *testing.T) {
	// Verify compile-time interface compliance
	var _ DistributedLocker = (*DistributedLock)(nil)
}
