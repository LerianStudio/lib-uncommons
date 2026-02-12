//go:build unit

package cron

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_DailyMidnight(t *testing.T) {
	t.Parallel()

	sched, err := Parse("0 0 * * *")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC), next)
}

func TestParse_EveryFiveMinutes(t *testing.T) {
	t.Parallel()

	sched, err := Parse("*/5 * * * *")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 10, 3, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 15, 10, 5, 0, 0, time.UTC), next)
}

func TestParse_DailySixThirty(t *testing.T) {
	t.Parallel()

	sched, err := Parse("30 6 * * *")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 7, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 16, 6, 30, 0, 0, time.UTC), next)
}

func TestParse_DailyNoon(t *testing.T) {
	t.Parallel()

	sched, err := Parse("0 12 * * *")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), next)
}

func TestParse_EveryMonday(t *testing.T) {
	t.Parallel()

	sched, err := Parse("0 0 * * 1")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, time.Monday, next.Weekday())
	assert.Equal(t, 0, next.Hour())
	assert.Equal(t, 0, next.Minute())
	assert.True(t, next.After(from))
}

func TestParse_FifteenthOfMonth(t *testing.T) {
	t.Parallel()

	sched, err := Parse("0 0 15 * *")
	require.NoError(t, err)

	from := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, 15, next.Day())
	assert.Equal(t, 0, next.Hour())
	assert.Equal(t, 0, next.Minute())
	assert.True(t, next.After(from))
}

func TestParse_Ranges(t *testing.T) {
	t.Parallel()

	sched, err := Parse("0 9-17 * * *")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, 9, next.Hour())
	assert.Equal(t, time.Date(2026, 1, 16, 9, 0, 0, 0, time.UTC), next)
}

func TestParse_Lists(t *testing.T) {
	t.Parallel()

	sched, err := Parse("0 6,12,18 * * *")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 7, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), next)
}

func TestParse_RangeWithStep(t *testing.T) {
	t.Parallel()

	sched, err := Parse("0 1-10/3 * * *")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 15, 1, 0, 0, 0, time.UTC), next)

	next, err = sched.Next(next)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 15, 4, 0, 0, 0, time.UTC), next)
}

func TestParse_InvalidExpression(t *testing.T) {
	t.Parallel()

	_, err := Parse("not-a-cron")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidExpression)
}

func TestParse_EmptyString(t *testing.T) {
	t.Parallel()

	_, err := Parse("")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidExpression)
}

func TestParse_TooFewFields(t *testing.T) {
	t.Parallel()

	_, err := Parse("0 0 *")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidExpression)
}

func TestParse_TooManyFields(t *testing.T) {
	t.Parallel()

	_, err := Parse("0 0 * * * *")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidExpression)
}

func TestParse_OutOfRangeValue(t *testing.T) {
	t.Parallel()

	_, err := Parse("60 0 * * *")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidExpression)
}

func TestParse_InvalidStep(t *testing.T) {
	t.Parallel()

	_, err := Parse("*/0 * * * *")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidExpression)
}

func TestParse_WhitespaceHandling(t *testing.T) {
	t.Parallel()

	sched, err := Parse("  0 0 * * *  ")
	require.NoError(t, err)

	from := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC), next)
}

func TestNext_ExhaustionReturnsError(t *testing.T) {
	t.Parallel()

	// Schedule for Feb 30 — a date that never exists.
	// This forces the iterator to exhaust maxIterations without finding a match.
	sched := &schedule{
		minutes: []int{0},
		hours:   []int{0},
		doms:    []int{30},
		months:  []int{2},
		dows:    []int{0, 1, 2, 3, 4, 5, 6},
	}

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next, err := sched.Next(from)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoMatch)
	assert.True(t, next.IsZero(), "expected zero time on exhaustion")
}
