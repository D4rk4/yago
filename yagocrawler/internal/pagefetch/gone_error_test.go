package pagefetch_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

func TestGoneStatus(t *testing.T) {
	cases := map[int]bool{
		http.StatusNotFound:            true,
		http.StatusGone:                true,
		http.StatusForbidden:           false,
		http.StatusInternalServerError: false,
		http.StatusTooManyRequests:     false,
		http.StatusOK:                  false,
	}
	for status, want := range cases {
		if got := pagefetch.GoneStatus(status); got != want {
			t.Errorf("GoneStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestGoneErrorWrapsPageRejected(t *testing.T) {
	err := error(&pagefetch.GoneError{Status: http.StatusNotFound})
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("GoneError must wrap ErrPageRejected, got %v", err)
	}

	var gone *pagefetch.GoneError
	if !errors.As(err, &gone) || gone.Status != http.StatusNotFound {
		t.Fatalf("errors.As did not recover the gone status: %#v", gone)
	}
	if err.Error() == "" {
		t.Fatal("GoneError.Error() must not be empty")
	}
}
