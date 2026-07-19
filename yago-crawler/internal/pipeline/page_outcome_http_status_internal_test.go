package pipeline

import (
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

func TestPageOutcomeHTTPStatusUsesOnlyKnownValidResponses(t *testing.T) {
	tests := []struct {
		err  error
		want uint32
	}{
		{fmt.Errorf("wrapped: %w", &pagefetch.HTTPStatusError{Status: 403}), 403},
		{&pagefetch.ThrottledError{Status: 429}, 429},
		{&pagefetch.GoneError{Status: 410}, 410},
		{&pagefetch.HTTPStatusError{Status: 99}, 0},
		{&pagefetch.ThrottledError{Status: 1000}, 0},
		{errors.New("no response"), 0},
	}
	for _, test := range tests {
		if got := pageOutcomeHTTPStatus(test.err); got != test.want {
			t.Errorf("HTTP status for %T = %d, want %d", test.err, got, test.want)
		}
	}
}
