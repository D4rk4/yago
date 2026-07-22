package yagomodel

import (
	"errors"
	"testing"
)

func TestSeedNetworkAddressAndEndpoint(t *testing.T) {
	host, err := ParseHost("192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	port, err := ParsePort("8090")
	if err != nil {
		t.Fatal(err)
	}
	seed := Seed{
		IP:   Some(host),
		Port: Some(port),
	}

	address, ok := seed.NetworkAddress()
	if !ok || address != "192.0.2.1:8090" {
		t.Fatalf("NetworkAddress = %q, %v", address, ok)
	}
	endpoint, err := seed.HTTPEndpoint("/yacy/hello.html")
	if err != nil {
		t.Fatal(err)
	}
	if got := endpoint.String(); got != "http://192.0.2.1:8090/yacy/hello.html" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestSeedNetworkAddressRequiresHostAndPort(t *testing.T) {
	if address, ok := (Seed{}).NetworkAddress(); ok || address != "" {
		t.Fatalf("empty seed NetworkAddress = %q, %v", address, ok)
	}
	if address, ok := (Seed{IP: Some(Host{}), Port: Some(Port(8090))}).NetworkAddress(); ok ||
		address != "" {
		t.Fatalf("empty host NetworkAddress = %q, %v", address, ok)
	}

	host, err := ParseHost("192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if address, ok := (Seed{IP: Some(host)}).NetworkAddress(); ok || address != "" {
		t.Fatalf("host-only seed NetworkAddress = %q, %v", address, ok)
	}
}

func TestSeedHTTPEndpointRequiresReachableAddress(t *testing.T) {
	if _, err := (Seed{}).HTTPEndpoint("/"); !errors.Is(err, ErrBadSeed) {
		t.Fatalf("HTTPEndpoint without address = %v, want ErrBadSeed", err)
	}
}
