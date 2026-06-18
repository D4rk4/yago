package infrastructure

import (
	"net"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	nets, err := parseTrustedProxies(" 10.0.0.0/8 , 192.168.1.1 , ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("got %d nets, want 2", len(nets))
	}
	if !nets[0].Contains(net.ParseIP("10.1.2.3")) {
		t.Error("CIDR should contain 10.1.2.3")
	}
	if !nets[1].Contains(net.ParseIP("192.168.1.1")) {
		t.Error("host net should contain its own IP")
	}
	if nets[1].Contains(net.ParseIP("192.168.1.2")) {
		t.Error("host net should not contain other IPs")
	}
}

func TestParseTrustedProxiesEmpty(t *testing.T) {
	nets, err := parseTrustedProxies("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nets != nil {
		t.Errorf("got %v, want nil", nets)
	}
}

func TestParseTrustedProxiesRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"not-an-ip", "10.0.0.0/99", "300.0.0.1"} {
		if _, err := parseTrustedProxies(raw); err == nil {
			t.Errorf("parseTrustedProxies(%q): expected error", raw)
		}
	}
}
