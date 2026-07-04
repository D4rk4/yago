package crawldispatch_test

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

func TestDispatchErrorUnwrapExposesCause(t *testing.T) {
	sentinel := errors.New("boom")
	de := &crawldispatch.DispatchError{Err: sentinel, Retryable: true}

	if !errors.Is(de, sentinel) {
		t.Fatal("errors.Is should unwrap the dispatch error to its cause")
	}
	if got := errors.Unwrap(de); !errors.Is(got, sentinel) {
		t.Fatalf("Unwrap = %v, want the sentinel cause", got)
	}
	if de.Error() != "boom" {
		t.Fatalf("Error = %q, want the cause message", de.Error())
	}
}
