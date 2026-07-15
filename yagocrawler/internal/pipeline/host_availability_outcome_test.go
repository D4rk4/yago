package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
)

type permanentOutcomeError struct{}

func (permanentOutcomeError) Error() string {
	return "permanent"
}

func (permanentOutcomeError) Permanent() bool {
	return true
}

func TestHostAvailabilityOutcomeClassifiesHostEvidence(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		failed   bool
		observed bool
	}{
		{
			name:     "throttled",
			err:      &pagefetch.ThrottledError{Status: 429},
			failed:   true,
			observed: true,
		},
		{
			name:     "server status",
			err:      &pagefetch.HTTPStatusError{Status: 502},
			failed:   true,
			observed: true,
		},
		{
			name:     "forbidden",
			err:      &pagefetch.HTTPStatusError{Status: 403},
			failed:   true,
			observed: true,
		},
		{name: "gone", err: &pagefetch.GoneError{Status: 404}},
		{name: "other client status", err: &pagefetch.HTTPStatusError{Status: 401}},
		{name: "unsupported", err: pagefetch.ErrUnsupportedContentType},
		{name: "robots", err: robots.ErrDisallowed},
		{
			name: "dns",
			err: fmt.Errorf("resolve: %w: %w", pagefetch.ErrPageRejected, &net.DNSError{
				Err: "timeout", Name: "example.com", IsTimeout: true,
			}),
			failed: true, observed: true,
		},
		{name: "permanent admission", err: permanentOutcomeError{}},
		{name: "plain rejection", err: pagefetch.ErrPageRejected},
		{name: "local browser", err: errors.New("browser launch failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failed, observed := hostAvailabilityOutcome(t.Context(), test.err)
			if failed != test.failed || observed != test.observed {
				t.Fatalf("outcome = failed %t observed %t", failed, observed)
			}
		})
	}
}

func TestHostAvailabilityOutcomeIgnoresCancelledWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	failed, observed := hostAvailabilityOutcome(ctx, &net.DNSError{
		Err: "timeout", Name: "example.com", IsTimeout: true,
	})
	if failed || observed {
		t.Fatalf("cancelled outcome = failed %t observed %t", failed, observed)
	}
}
