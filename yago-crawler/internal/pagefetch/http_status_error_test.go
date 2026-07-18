package pagefetch

import (
	"errors"
	"fmt"
	"testing"
)

func TestHTTPStatusErrorCarriesRejectedStatus(t *testing.T) {
	err := fmt.Errorf("fetch: %w", &HTTPStatusError{Status: 502})
	statusError, ok := AsHTTPStatus(err)
	if !ok || statusError.Status != 502 || !errors.Is(err, ErrPageRejected) {
		t.Fatalf("status error = %v, %t", statusError, ok)
	}
	if _, ok := AsHTTPStatus(errors.New("plain")); ok {
		t.Fatal("plain error reported as an HTTP status")
	}
}
