package yacymodel

import (
	"errors"
	"testing"
)

const sampleSeed = "{Flags=    ,Hash=ABCDEFGHIJKL,IP=192.0.2.1,Name=testpeer,PeerType=senior,Port=8090}"

func TestParseSeedRoundTrip(t *testing.T) {
	seed, err := ParseSeed(t.Context(), sampleSeed)
	if err != nil {
		t.Fatal(err)
	}
	if got := seed.String(); got != sampleSeed {
		t.Errorf("round trip:\n got %q\nwant %q", got, sampleSeed)
	}
}

func TestSeedTypedFields(t *testing.T) {
	seed, err := ParseSeed(t.Context(), sampleSeed)
	if err != nil {
		t.Fatal(err)
	}
	if seed.Hash != "ABCDEFGHIJKL" {
		t.Errorf("Hash = %q", seed.Hash)
	}
	if pt, ok := seed.PeerType.Get(); !ok || pt != PeerSenior {
		t.Errorf("PeerType = %q, %v", pt, ok)
	}
	if _, ok := seed.Flags.Get(); !ok {
		t.Errorf("Flags absent")
	}
	if port, ok := seed.Port.Get(); !ok || port != 8090 {
		t.Errorf("Port = %d, %v", port, ok)
	}
}

func TestParseSeedBad(t *testing.T) {
	if _, err := ParseSeed(t.Context(), "=novalue"); !errors.Is(err, ErrBadSeed) {
		t.Fatalf("ParseSeed bad = %v, want ErrBadSeed", err)
	}
}

func TestParseSeedMissingHash(t *testing.T) {
	if _, err := ParseSeed(t.Context(), "{Port=8090}"); !errors.Is(err, ErrBadSeed) {
		t.Fatalf("ParseSeed missing hash = %v, want ErrBadSeed", err)
	}
}

func TestParseSeedSkipsEmptyPairs(t *testing.T) {
	seed, err := ParseSeed(t.Context(), "{Hash=ABCDEFGHIJKL,,Port=8090}")
	if err != nil {
		t.Fatal(err)
	}
	if seed.Hash != "ABCDEFGHIJKL" {
		t.Errorf("Hash = %q", seed.Hash)
	}
	if port, ok := seed.Port.Get(); !ok || port != 8090 {
		t.Errorf("Port = %d, %v", port, ok)
	}
}

func TestParseSeedAcceptsBareMap(t *testing.T) {
	seed, err := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,Port=8090")
	if err != nil {
		t.Fatal(err)
	}
	if port, ok := seed.Port.Get(); seed.Hash != "ABCDEFGHIJKL" || !ok || port != 8090 {
		t.Errorf("seed = %v", seed)
	}
}

func TestParseSeedPortInvalid(t *testing.T) {
	if _, err := ParseSeed(
		t.Context(),
		"Hash=ABCDEFGHIJKL,Port=notnum",
	); !errors.Is(
		err,
		ErrBadSeed,
	) {
		t.Fatalf("ParseSeed bad port = %v, want ErrBadSeed", err)
	}
}

func TestParseSeedEmptyIP6(t *testing.T) {
	seed, err := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,IP6=")
	if err != nil {
		t.Fatal(err)
	}
	if seed.Hash != "ABCDEFGHIJKL" {
		t.Errorf("Hash = %q", seed.Hash)
	}
	if _, ok := seed.IP6.Get(); ok {
		t.Error("IP6 should not be present when empty")
	}
}

func TestParseSeedEmptyIP(t *testing.T) {
	seed, err := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,IP=")
	if err != nil {
		t.Fatal(err)
	}
	if seed.Hash != "ABCDEFGHIJKL" {
		t.Errorf("Hash = %q", seed.Hash)
	}
	if _, ok := seed.IP.Get(); ok {
		t.Error("IP should not be present when empty")
	}
}
