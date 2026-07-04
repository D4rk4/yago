package yagomodel

import (
	"errors"
	"strings"
	"testing"
)

func TestParseHostAcceptsIPAddressAndHostname(t *testing.T) {
	ip, err := ParseHost("2001:db8::1")
	if err != nil {
		t.Fatal(err)
	}
	if got := ip.String(); got != "2001:db8::1" {
		t.Fatalf("ip host = %q", got)
	}

	name, err := ParseHost("peer.example")
	if err != nil {
		t.Fatal(err)
	}
	if got := name.String(); got != "peer.example" {
		t.Fatalf("hostname = %q", got)
	}
}

func TestParseHostRejectsBadHostnames(t *testing.T) {
	for _, raw := range []string{
		"",
		"-peer.example",
		"peer-.example",
		"peer..example",
		"peer.example!",
		strings.Repeat("a", 64) + ".example",
		strings.Repeat("a", 256),
	} {
		if _, err := ParseHost(raw); !errors.Is(err, ErrBadHost) {
			t.Fatalf("ParseHost(%q) = %v, want ErrBadHost", raw, err)
		}
	}
}

func TestZeroHostString(t *testing.T) {
	if got := (Host{}).String(); got != "" {
		t.Fatalf("zero host string = %q", got)
	}
}
