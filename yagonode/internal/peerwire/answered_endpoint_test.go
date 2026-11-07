package peerwire

import (
	"errors"
	"testing"
)

func TestParseEndpointOriginAcceptsBareOrigins(t *testing.T) {
	cases := map[string]Endpoint{
		"http://example.org:8090":    {Scheme: "http", Host: "example.org", Port: "8090"},
		"https://example.org:443":    {Scheme: "https", Host: "example.org", Port: "443"},
		"http://192.0.2.7:8090":      {Scheme: "http", Host: "192.0.2.7", Port: "8090"},
		"https://[2001:db8::7]:8443": {Scheme: "https", Host: "2001:db8::7", Port: "8443"},
	}
	for origin, want := range cases {
		got, err := ParseEndpointOrigin(origin)
		if err != nil || got != want {
			t.Fatalf("ParseEndpointOrigin(%q) = %+v, %v; want %+v", origin, got, err, want)
		}
		if got.Origin() != origin {
			t.Fatalf("Origin() = %q, want %q", got.Origin(), origin)
		}
	}
}

func TestParseEndpointOriginRefusesNonOriginRecords(t *testing.T) {
	refused := []string{
		"%zz",
		"ftp://example.org:21",
		"HTTP://example.org:8090",
		"http://user@example.org:8090",
		"http://example.org:8090/",
		"http://example.org:8090/yacy/hello.html",
		"http://example.org:8090?peer=1",
		"http://example.org:8090#fragment",
		"http://example.org",
		"http://example.org:0",
		"http://example.org:65536",
		"http://exa_mple.org:8090",
		"",
	}
	for _, origin := range refused {
		if endpoint, err := ParseEndpointOrigin(origin); !errors.Is(err, ErrInvalidEndpoint) {
			t.Fatalf("ParseEndpointOrigin(%q) = %+v, %v; want refusal", origin, endpoint, err)
		}
	}
}
