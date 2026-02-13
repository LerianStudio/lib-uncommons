package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	libCommons "github.com/LerianStudio/lib-uncommons/v2/uncommons"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry"
	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
)

// DistributedLock provides distributed locking capabilities using Redis and the RedLock algorithm.
// This implementation ensures mutual exclusion across multiple service instances, preventing race
// conditions in critical sections such as:
// - Password update operations
// - Cache invalidation
// - Rate limiting checks
// - Any other operation requiring distributed coordination
//
// The RedLock algorithm provides strong guarantees even in the presence of:
// - Network partitions
// - Process crashes
// - Clock drift
//
// Example usage:
//
//	lock, err := redis.NewDistributedLock(redisClient)
//	if err != nil {
//	    return err
//	}
//
//	err = lock.WithLock(ctx, "lock:user:123", func(ctx context.Context) error {
//	    // Critical section - only one instance will execute this at a time
//	    return updateUser(123)
//	})
type DistributedLock struct {
	redsync *redsync.Redsync
}

// LockOptions configures lock behavior for advanced use cases.
// Use DefaultLockOptions() for sensible defaults.
type LockOptions struct {
	// Expiry is how long the lock is held before auto-expiring (prevents deadlocks)
	// Default: 10 seconds
	Expiry time.Duration

	// Tries is the number of attempts to acquire the lock before giving up
	// Default: 3
	Tries int

	// RetryDelay is the delay between retry attempts
	// Default: 500ms
	RetryDelay time.Duration

	// DriftFactor accounts for clock drift in distributed systems (RedLock algorithm)
	// Default: 0.01 (1%)
	DriftFactor float64
}

// DefaultLockOptions returns production-ready defaults for distributed locking.
// These values are tuned for typical microservice scenarios with:
// - Operations completing within seconds
// - Network latency < 100ms
// - Acceptable retry overhead
func DefaultLockOptions() LockOptions {
	return LockOptions{
		Expiry:      10 * time.Second,
		Tries:       3,
		RetryDelay:  500 * time.Millisecond,
		DriftFactor: 0.01,
	}
}

// RateLimiterLockOptions returns optimized defaults for rate limiter locking.
// These values are tuned for short, fast operations like rate limiting:
// - Quick operations (< 100ms)
// - Fast retry for better throughput
// - Lower expiry to reduce contention
func RateLimiterLockOptions() LockOptions {
	return LockOptions{
		Expiry:      2 * time.Second,
		Tries:       2,
		RetryDelay:  100 * time.Millisecond,
		DriftFactor: 0.01,
	}
}

// NewDistributedLock creates a new distributed lock manager.
// The lock manager uses the RedLock algorithm for distributed consensus.
//
// Thread-safe: Yes - multiple goroutines can use the same DistributedLock instance.
//
// Example:
//
//	lock, err := redis.NewDistributedLock(redisClient)
//	if err != nil {
//	    return fmt.Errorf("failed to initialize lock: %w", err)
//	}
func NewDistributedLock(conn *Client) (*DistributedLock, error) {
	if conn == nil {
		return nil, errors.New("redis client is nil")
	}

	ctx := context.Background()

	client, err := conn.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get redis client: %w", err)
	}

	pool := goredis.NewPool(client)
	rs := redsync.New(pool)

	return &DistributedLock{
		redsync: rs,
	}, nil
}

// WithLock executes a function while holding a distributed lock.
// The lock is automatically released when the function returns, even on panic.
//
// Parameters:
//   - ctx: context for cancellation and tracing
//   - lockKey: unique identifier for the lock (e.g., "lock:user:123")
//   - fn: function to execute under lock
//
// Returns:
//   - error: from fn() or lock acquisition failure
//
// Example:
//
//	err := lock.WithLock(ctx, "lock:user:password:123", func(ctx context.Context) error {
//	    return updatePassword(123, newPassword)
//	})
func (dl *DistributedLock) WithLock(ctx context.Context, lockKey string, fn func(context.Context) error) error {
	if dl == nil {
		return errors.New("distributed lock is nil")
	}

	return dl.WithLockOptions(ctx, lockKey, DefaultLockOptions(), fn)
}

// WithLockOptions executes a function while holding a distributed lock with custom options.
// Use this when you need fine-grained control over lock behavior.
//
// Example with custom timeout:
//
//	opts := redis.LockOptions{
//	    Expiry:     30 * time.Second, // Long-running operation
//	    Tries:      5,                 // More aggressive retries
//	    RetryDelay: 1 * time.Second,
//	}
//	err := lock.WithLockOptions(ctx, "lock:report:generation", opts, func(ctx context.Context) error {
//	    return generateReport()
//	})
func (dl *DistributedLock) WithLockOptions(ctx context.Context, lockKey string, opts LockOptions, fn func(context.Context) error) error {
	if dl == nil {
		return errors.New("distributed lock is nil")
	}

	if dl.redsync == nil {
		return errors.New("distributed lock is not initialized")
	}

	if fn == nil {
		return errors.New("fn is nil")
	}

	if strings.TrimSpace(lockKey) == "" {
		return errors.New("lock key cannot be empty")
	}

	if err := validateLockOptions(opts); err != nil {
		return err
	}

	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)
	safeLockKey := safeLockKeyForLogs(lockKey)

	ctx, span := tracer.Start(ctx, "distributed_lock.with_lock")
	defer span.End()

	// Create mutex with configured options
	mutex := dl.redsync.NewMutex(
		lockKey,
		redsync.WithExpiry(opts.Expiry),
		redsync.WithTries(opts.Tries),
		redsync.WithRetryDelay(opts.RetryDelay),
		redsync.WithDriftFactor(opts.DriftFactor),
	)

	logger.Log(ctx, log.LevelDebug, "attempting to acquire lock", log.String("lock_key", safeLockKey))

	// Try to acquire the lock
	if err := mutex.LockContext(ctx); err != nil {
		logger.Log(ctx, log.LevelError, "failed to acquire lock", log.String("lock_key", safeLockKey), log.Err(err))
		opentelemetry.HandleSpanError(span, "Failed to acquire lock", err)

		return fmt.Errorf("failed to acquire lock %s: %w", safeLockKey, err)
	}

	logger.Log(ctx, log.LevelDebug, "lock acquired", log.String("lock_key", safeLockKey))

	// Ensure lock is released even if function panics
	defer func() {
		if ok, err := mutex.UnlockContext(ctx); !ok || err != nil {
			logger.Log(ctx, log.LevelError, "failed to release lock", log.String("lock_key", safeLockKey), log.Bool("unlock_ok", ok), log.Err(err))
		} else {
			logger.Log(ctx, log.LevelDebug, "lock released", log.String("lock_key", safeLockKey))
		}
	}()

	// Execute the function while holding the lock
	logger.Log(ctx, log.LevelDebug, "executing function under lock", log.String("lock_key", safeLockKey))

	if err := fn(ctx); err != nil {
		logger.Log(ctx, log.LevelError, "function execution failed under lock", log.String("lock_key", safeLockKey), log.Err(err))
		opentelemetry.HandleSpanError(span, "Function execution failed", err)

		return err
	}

	logger.Log(ctx, log.LevelDebug, "function completed successfully under lock", log.String("lock_key", safeLockKey))

	return nil
}

// TryLock attempts to acquire a lock without retrying.
// Returns the mutex and true if lock was acquired, false if lock is busy.
// Returns an error for unexpected failures (network errors, context cancellation, etc.)
//
// Use this when you want to skip the operation if the lock is busy:
//
//	mutex, acquired, err := lock.TryLock(ctx, "lock:cache:refresh")
//	if err != nil {
//	    // Unexpected error (network, context cancellation, etc.) - should be propagated
//	    return fmt.Errorf("failed to attempt lock acquisition: %w", err)
//	}
//	if !acquired {
//	    logger.Info("Lock busy, skipping cache refresh")
//	    return nil
//	}
//	defer lock.Unlock(ctx, mutex)
//	// Perform cache refresh...
func (dl *DistributedLock) TryLock(ctx context.Context, lockKey string) (*redsync.Mutex, bool, error) {
	if dl == nil {
		return nil, false, errors.New("distributed lock is nil")
	}

	if dl.redsync == nil {
		return nil, false, errors.New("distributed lock is not initialized")
	}

	if strings.TrimSpace(lockKey) == "" {
		return nil, false, errors.New("lock key cannot be empty")
	}

	logger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)
	safeLockKey := safeLockKeyForLogs(lockKey)

	ctx, span := tracer.Start(ctx, "distributed_lock.try_lock")
	defer span.End()

	defaultOpts := DefaultLockOptions()

	mutex := dl.redsync.NewMutex(
		lockKey,
		redsync.WithExpiry(defaultOpts.Expiry),
		redsync.WithTries(1), // Only try once
	)

	if err := mutex.LockContext(ctx); err != nil {
		// Check if this is a lock contention error (expected behavior)
		// redsync returns different error messages for lock contention:
		// - "lock already taken" when another process holds the lock
		// - "redsync: failed to acquire lock" as the base error
		errMsg := err.Error()
		isLockContention := errors.Is(err, redsync.ErrFailed) ||
			strings.Contains(errMsg, "lock already taken") ||
			strings.Contains(errMsg, "failed to acquire lock")

		if isLockContention {
			logger.Log(ctx, log.LevelDebug, "lock already held by another process", log.String("lock_key", safeLockKey))
			return nil, false, nil
		}

		// Any other error (e.g., network, context cancellation) is an actual failure
		// and should be propagated to the caller.
		logger.Log(ctx, log.LevelDebug, "could not acquire lock", log.String("lock_key", safeLockKey), log.Err(err))
		opentelemetry.HandleSpanError(span, "Failed to attempt lock acquisition", err)

		return nil, false, fmt.Errorf("failed to attempt lock acquisition for %s: %w", safeLockKey, err)
	}

	logger.Log(ctx, log.LevelDebug, "lock acquired", log.String("lock_key", safeLockKey))

	return mutex, true, nil
}

// Unlock releases a previously acquired lock.
// This is only needed if you use TryLock(). WithLock() handles unlocking automatically.
func (dl *DistributedLock) Unlock(ctx context.Context, mutex *redsync.Mutex) error {
	if dl == nil {
		return errors.New("distributed lock is nil")
	}

	logger := libCommons.NewLoggerFromContext(ctx)

	if mutex == nil {
		return errors.New("mutex is nil")
	}

	ok, err := mutex.UnlockContext(ctx)
	if err != nil {
		logger.Log(ctx, log.LevelError, "failed to unlock mutex", log.Err(err))
		return err
	}

	if !ok {
		logger.Log(ctx, log.LevelWarn, "mutex was not locked or already expired")
		return errors.New("mutex was not locked")
	}

	return nil
}

func validateLockOptions(opts LockOptions) error {
	if opts.Expiry <= 0 {
		return errors.New("lock expiry must be greater than 0")
	}

	if opts.Tries < 1 {
		return errors.New("lock tries must be at least 1")
	}

	if opts.RetryDelay < 0 {
		return errors.New("lock retry delay cannot be negative")
	}

	if opts.DriftFactor < 0 || opts.DriftFactor >= 1 {
		return errors.New("lock drift factor must be between 0 (inclusive) and 1 (exclusive)")
	}

	return nil
}

func safeLockKeyForLogs(lockKey string) string {
	const maxLockKeyLogLength = 128

	safeLockKey := strconv.QuoteToASCII(lockKey)
	if len(safeLockKey) <= maxLockKeyLogLength {
		return safeLockKey
	}

	return safeLockKey[:maxLockKeyLogLength] + "...(truncated)"
}
