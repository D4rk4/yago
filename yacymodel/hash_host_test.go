package yacymodel

import "testing"

func TestHashHostHash(t *testing.T) {
	if got := Hash("0123456789AB").HostHash(); got != "6789AB" {
		t.Fatalf("HostHash() = %q, want %q", got, "6789AB")
	}
}

func TestHashHostHashUsesYaCyURLHashSuffix(t *testing.T) {
	hash := Hash("local1hostH6")
	if got := hash.HostHash(); got != "hostH6" {
		t.Fatalf("HostHash() = %q, want hostH6", got)
	}
}

func TestHashHostHashInvalidLength(t *testing.T) {
	if got := Hash("short").HostHash(); got != "" {
		t.Fatalf("HostHash() = %q, want empty", got)
	}
}
