package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-github/v69/github"
	"github.com/stretchr/testify/assert"
)

var errGeneric = fmt.Errorf("some error")

func TestWait_NotRateLimited(t *testing.T) {
	assert.False(t, Wait(context.Background(), errGeneric))
}

func TestWait_NilError(t *testing.T) {
	assert.False(t, Wait(context.Background(), nil))
}

func TestWait_RateLimitError(t *testing.T) {
	resetTime := time.Now().Add(100 * time.Millisecond)
	err := &github.RateLimitError{
		Rate: github.Rate{
			Remaining: 0,
			Reset:     github.Timestamp{Time: resetTime},
		},
	}

	start := time.Now()
	ok := Wait(context.Background(), err)
	elapsed := time.Since(start)

	assert.True(t, ok)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond, "waited only %v", elapsed)
}

func TestWait_CancelledContext(t *testing.T) {
	resetTime := time.Now().Add(10 * time.Second)
	err := &github.RateLimitError{
		Rate: github.Rate{
			Remaining: 0,
			Reset:     github.Timestamp{Time: resetTime},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.False(t, Wait(ctx, err))
}
