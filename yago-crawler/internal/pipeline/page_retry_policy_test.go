package pipeline

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

func TestRetryableFetchFailureAdmitsTransientFetchesAndRejectsPermanentOutcomes(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		err     error
		attempt uint8
		cancel  bool
		want    bool
	}{
		{name: "throttle", err: &pagefetch.ThrottledError{Status: 429}, want: true},
		{name: "server error", err: &pagefetch.HTTPStatusError{Status: 500}, want: true},
		{name: "request timeout", err: &pagefetch.HTTPStatusError{Status: 408}, want: true},
		{name: "network timeout", err: &net.DNSError{IsTimeout: true}, want: true},
		{name: "truncated body", err: io.ErrUnexpectedEOF, want: true},
		{name: "not found", err: &pagefetch.HTTPStatusError{Status: 404}},
		{name: "robots denial", err: errors.New("robots denial")},
		{
			name:    "last retry exhausted",
			err:     &pagefetch.HTTPStatusError{Status: 503},
			attempt: maximumPageRetryAttempts,
		},
		{name: "caller cancelled", err: &pagefetch.HTTPStatusError{Status: 503}, cancel: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			deadline, retry := retryableFetchFailure(ctx, test.err, test.attempt, now)
			if retry != test.want {
				t.Fatalf("retry = %v, want %v", retry, test.want)
			}
			if retry && !deadline.After(now) {
				t.Fatalf("retry deadline = %s, want after %s", deadline, now)
			}
			if !retry && !deadline.IsZero() {
				t.Fatalf("permanent outcome produced retry deadline %s", deadline)
			}
		})
	}
}

func TestPageRetryDeadlineHonorsServerWaitAndCapsBackoff(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	serverWait := 2 * time.Hour
	if got := pageRetryDeadline(now, 0, serverWait); got.Before(now.Add(serverWait)) {
		t.Fatalf("server retry deadline = %s, shorter than %s", got, serverWait)
	}
	deadline := pageRetryDeadline(now, 255, 0)
	minimum := now.Add(maximumPageRetryDelay)
	maximum := now.Add(maximumPageRetryDelay + maximumPageRetryDelay/2)
	if deadline.Before(minimum) || deadline.After(maximum) {
		t.Fatalf("maximum retry deadline = %s", deadline)
	}
}

func TestPageRetryDeadlineAddsBoundedJitterBelowCap(t *testing.T) {
	previous := pageRetryJitterRandomInt
	t.Cleanup(func() { pageRetryJitterRandomInt = previous })
	maximum := basePageRetryDelay / 2
	pageRetryJitterRandomInt = func(got *big.Int) (*big.Int, error) {
		want := big.NewInt(int64(maximum))
		if got.Cmp(want) != 0 {
			t.Fatalf("jitter maximum = %s, want %s", got, want)
		}
		return new(big.Int).Sub(got, big.NewInt(1)), nil
	}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadline := pageRetryDeadline(now, 0, 0)
	want := now.Add(basePageRetryDelay + maximum - time.Nanosecond)
	if !deadline.Equal(want) {
		t.Fatalf("retry deadline = %s, want %s", deadline, want)
	}
}

func TestPageRetryDeadlineDoesNotJitterAtCap(t *testing.T) {
	previous := pageRetryJitterRandomInt
	t.Cleanup(func() { pageRetryJitterRandomInt = previous })
	called := false
	pageRetryJitterRandomInt = func(*big.Int) (*big.Int, error) {
		called = true
		return big.NewInt(1), nil
	}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadline := pageRetryDeadline(now, 255, 0)
	want := now.Add(maximumPageRetryDelay)
	if !deadline.Equal(want) {
		t.Fatalf("capped retry deadline = %s, want %s", deadline, want)
	}
	if called {
		t.Fatal("retry jitter source ran after the backoff cap")
	}
}

func TestPageRetryDeadlineUsesBaseDelayWhenRandomSourceFails(t *testing.T) {
	previous := pageRetryJitterRandomInt
	t.Cleanup(func() { pageRetryJitterRandomInt = previous })
	pageRetryJitterRandomInt = func(*big.Int) (*big.Int, error) {
		return nil, errors.New("random source unavailable")
	}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	deadline := pageRetryDeadline(now, 0, 0)
	want := now.Add(basePageRetryDelay)
	if !deadline.Equal(want) {
		t.Fatalf("retry deadline = %s, want %s", deadline, want)
	}
}

func TestSecurePageRetryJitterStaysBelowMaximum(t *testing.T) {
	maximum := time.Second
	for range 8 {
		jitter, err := securePageRetryJitter(maximum)
		if err != nil {
			t.Fatalf("secure retry jitter: %v", err)
		}
		if jitter < 0 || jitter >= maximum {
			t.Fatalf("retry jitter = %s, want [0, %s)", jitter, maximum)
		}
	}
}
