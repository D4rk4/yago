package yacycrawlcontract

import (
	"errors"
	"testing"
)

func TestWrapMarshalError(t *testing.T) {
	if err := wrapMarshalError("marshal value", nil); err != nil {
		t.Fatalf("nil error wrapped to %v", err)
	}
	sentinel := errors.New("failed")
	if err := wrapMarshalError("marshal value", sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("wrapped error = %v, want %v", err, sentinel)
	}
}
