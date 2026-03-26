package ratelimit

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/go-github/v69/github"
)

// Wait checks if err is a GitHub rate limit error and sleeps until the reset time.
// Returns true if it waited (caller should retry), false otherwise.
func Wait(ctx context.Context, err error) bool {
	var rlErr *github.RateLimitError
	if errors.As(err, &rlErr) {
		wait := time.Until(rlErr.Rate.Reset.Time) + time.Second
		if wait < 0 {
			wait = time.Minute
		}

		slog.Warn("rate limited", "reset", rlErr.Rate.Reset.Time, "wait", wait)

		select {
		case <-time.After(wait):
			return true
		case <-ctx.Done():
			return false
		}
	}

	var abuseErr *github.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		wait := abuseErr.GetRetryAfter()
		if wait == 0 {
			wait = time.Minute
		}

		slog.Warn("abuse rate limited", "wait", wait)

		select {
		case <-time.After(wait):
			return true
		case <-ctx.Done():
			return false
		}
	}

	return false
}
