package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-github/v69/github"
)

var errGeneric = fmt.Errorf("some error")

func TestWait_NotRateLimited(t *testing.T) {
	if Wait(context.Background(), errGeneric) {
		t.Error("expected false for non-rate-limit error")
	}
}

func TestWait_NilError(t *testing.T) {
	if Wait(context.Background(), nil) {
		t.Error("expected false for nil error")
	}
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

	if !ok {
		t.Error("expected true for rate limit error")
	}

	if elapsed < 100*time.Millisecond {
		t.Errorf("waited only %v, expected >= 100ms", elapsed)
	}
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

	if Wait(ctx, err) {
		t.Error("expected false for cancelled context")
	}
}
