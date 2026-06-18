package yacymodel

import "testing"

func TestHashHostHash(t *testing.T) {
	if got := Hash("0123456789AB").HostHash(); got != "6789AB" {
		t.Fatalf("HostHash() = %q, want %q", got, "6789AB")
	}
}

func TestHashHostHashInvalidLength(t *testing.T) {
	if got := Hash("short").HostHash(); got != "" {
		t.Fatalf("HostHash() = %q, want empty", got)
	}
}
