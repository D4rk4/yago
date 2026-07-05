package yagomodel

import (
	"testing"
)

func sslSeed(t *testing.T) Seed {
	t.Helper()
	host, err := ParseHost("192.0.2.7")
	if err != nil {
		t.Fatalf("ParseHost: %v", err)
	}
	port, err := ParsePort("8090")
	if err != nil {
		t.Fatalf("ParsePort: %v", err)
	}
	sslPort, err := ParsePort("8443")
	if err != nil {
		t.Fatalf("ParsePort: %v", err)
	}

	return Seed{
		Hash:    "ABCDEFGHIJKL",
		IP:      Some(host),
		Port:    Some(port),
		PortSSL: Some(sslPort),
		Version: Some(YaCyVersion("1.941")),
		Flags:   Some(ZeroFlags().Set(FlagSSLAvailable, true)),
	}
}

func TestSeedSSLAvailable(t *testing.T) {
	if !sslSeed(t).SSLAvailable() {
		t.Fatal("seed with ssl flag, port, and current version should advertise ssl")
	}

	for _, item := range []struct {
		name   string
		mutate func(*Seed)
	}{
		{name: "missing version", mutate: func(s *Seed) { s.Version = None[YaCyVersion]() }},
		{
			name:   "non-numeric version",
			mutate: func(s *Seed) { s.Version = Some(YaCyVersion("dev")) },
		},
		{
			name:   "version before the ssl flag existed",
			mutate: func(s *Seed) { s.Version = Some(YaCyVersion("1.4")) },
		},
		{name: "missing flags", mutate: func(s *Seed) { s.Flags = None[Flags]() }},
		{name: "unset ssl flag", mutate: func(s *Seed) { s.Flags = Some(ZeroFlags()) }},
		{name: "missing ssl port", mutate: func(s *Seed) { s.PortSSL = None[Port]() }},
	} {
		seed := sslSeed(t)
		item.mutate(&seed)
		if seed.SSLAvailable() {
			t.Fatalf("%s: seed should not advertise ssl", item.name)
		}
	}
}

func TestSeedProtocolEndpointsPreferHTTPS(t *testing.T) {
	endpoints, err := sslSeed(t).ProtocolEndpoints("/yacy/hello.html", true)
	if err != nil {
		t.Fatalf("ProtocolEndpoints: %v", err)
	}
	if len(endpoints) != 2 ||
		endpoints[0].String() != "https://192.0.2.7:8443/yacy/hello.html" ||
		endpoints[1].String() != "http://192.0.2.7:8090/yacy/hello.html" {
		t.Fatalf("endpoints = %v", endpoints)
	}
}

func TestSeedProtocolEndpointsPlainOnly(t *testing.T) {
	plain := "http://192.0.2.7:8090/yacy/hello.html"

	// Preference off keeps the plain endpoint even when ssl is advertised.
	endpoints, err := sslSeed(t).ProtocolEndpoints("/yacy/hello.html", false)
	if err != nil {
		t.Fatalf("ProtocolEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].String() != plain {
		t.Fatalf("preference off endpoints = %v", endpoints)
	}

	// Preference on without advertised ssl also keeps the plain endpoint.
	seed := sslSeed(t)
	seed.Flags = Some(ZeroFlags())
	endpoints, err = seed.ProtocolEndpoints("/yacy/hello.html", true)
	if err != nil {
		t.Fatalf("ProtocolEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].String() != plain {
		t.Fatalf("no-ssl endpoints = %v", endpoints)
	}
}

func TestSeedProtocolEndpointsWithoutAddress(t *testing.T) {
	if _, err := (Seed{Hash: "ABCDEFGHIJKL"}).ProtocolEndpoints(
		"/yacy/hello.html",
		true,
	); err == nil {
		t.Fatal("expected error for seed without a reachable address")
	}
}

func TestYaCyVersionFloat(t *testing.T) {
	value, err := YaCyVersion("1.941").Float()
	if err != nil || value != 1.941 {
		t.Fatalf("Float = %v, %v", value, err)
	}
	if _, err := YaCyVersion("dev").Float(); err == nil {
		t.Fatal("expected error for non-numeric version")
	}
}
