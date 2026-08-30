package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryUntilSuccessRetriesTransientErrors(t *testing.T) {
	ctx := context.Background()
	attempts := 0

	err := retryUntilSuccess(ctx, retryOptions{
		Attempts: 3,
		Delay:    time.Hour,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	}, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("not ready")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("retryUntilSuccess returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryUntilSuccessReturnsLastErrorAfterAttempts(t *testing.T) {
	ctx := context.Background()
	want := errors.New("still not ready")

	err := retryUntilSuccess(ctx, retryOptions{
		Attempts: 2,
		Delay:    time.Hour,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	}, func() error {
		return want
	})

	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapping %v", err, want)
	}
}
