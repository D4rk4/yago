package yacymodel

import (
	"errors"
	"testing"
)

func TestParsePortRejectsBadValues(t *testing.T) {
	for _, raw := range []string{"notnum", "0", "65536"} {
		if _, err := ParsePort(raw); !errors.Is(err, ErrBadPort) {
			t.Fatalf("ParsePort(%q) = %v, want ErrBadPort", raw, err)
		}
	}
}
