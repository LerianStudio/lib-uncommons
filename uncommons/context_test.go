//go:build unit

package uncommons

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithTimeoutSafe_NilParent(t *testing.T) {
	ctx, cancel, err := WithTimeoutSafe(nil, 5*time.Second)

	if ctx != nil {
		t.Error("expected nil context")
	}

	if err == nil {
		t.Fatal("expected error for nil parent")
	}

	if !errors.Is(err, ErrNilParentContext) {
		t.Errorf("expected ErrNilParentContext, got %v", err)
	}

	if cancel != nil {
		t.Error("expected nil cancel function")
	}
}

func TestWithTimeoutSafe_Success(t *testing.T) {
	parent := context.Background()
	timeout := 5 * time.Second

	ctx, cancel, err := WithTimeoutSafe(parent, timeout)
	defer cancel()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have a deadline")
	}

	timeUntil := time.Until(deadline)
	if timeUntil < 4800*time.Millisecond || timeUntil > 5200*time.Millisecond {
		t.Errorf("deadline not within expected range: got %.2fs remaining, expected ~5s", timeUntil.Seconds())
	}
}

func TestWithTimeoutSafe_ParentDeadlineShorter(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer parentCancel()

	ctx, cancel, err := WithTimeoutSafe(parent, 10*time.Second)
	defer cancel()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have a deadline")
	}

	timeUntil := time.Until(deadline)
	if timeUntil > 2*time.Second || timeUntil < 1*time.Second {
		t.Errorf("expected deadline to be ~2s from now, got %v", timeUntil)
	}
}

func TestWithTimeoutSafe_CancelWorks(t *testing.T) {
	parent := context.Background()
	ctx, cancel, err := WithTimeoutSafe(parent, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cancel()

	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("context was not cancelled")
	}

	if ctx.Err() != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", ctx.Err())
	}
}

func TestWithTimeoutSafe_ZeroTimeout(t *testing.T) {
	parent := context.Background()
	ctx, cancel, err := WithTimeoutSafe(parent, 0)
	defer cancel()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	// Zero timeout should create an already-expired context
	select {
	case <-ctx.Done():
		// Expected - context should be done immediately or very soon
	case <-time.After(100 * time.Millisecond):
		t.Error("expected context to be done with zero timeout")
	}
}

func TestWithTimeoutSafe_NegativeTimeout(t *testing.T) {
	parent := context.Background()
	ctx, cancel, err := WithTimeoutSafe(parent, -1*time.Second)
	defer cancel()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	// Negative timeout should create an already-expired context
	select {
	case <-ctx.Done():
		// Expected - context should be done immediately
	case <-time.After(100 * time.Millisecond):
		t.Error("expected context to be done with negative timeout")
	}
}
