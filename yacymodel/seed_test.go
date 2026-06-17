package yacymodel

import (
	"errors"
	"testing"
)

const sampleSeed = "Flags=    ,Hash=ABCDEFGHIJKL,IP=192.0.2.1,Name=testpeer,PeerType=senior,Port=8090"

func TestParseSeedRoundTrip(t *testing.T) {
	seed, err := ParseSeed(sampleSeed)
	if err != nil {
		t.Fatal(err)
	}
	if got := seed.String(); got != sampleSeed {
		t.Errorf("round trip:\n got %q\nwant %q", got, sampleSeed)
	}
}

func TestSeedTypedAccessors(t *testing.T) {
	seed, err := ParseSeed(sampleSeed)
	if err != nil {
		t.Fatal(err)
	}
	h, err := seed.Hash()
	if err != nil || h != "ABCDEFGHIJKL" {
		t.Errorf("Hash() = %q, %v", h, err)
	}
	pt, err := seed.PeerType()
	if err != nil || pt != PeerSenior {
		t.Errorf("PeerType() = %q, %v", pt, err)
	}
	if _, err := seed.Flags(); err != nil {
		t.Errorf("Flags() = %v", err)
	}
	port, err := seed.Port()
	if err != nil || port != 8090 {
		t.Errorf("Port() = %d, %v", port, err)
	}
}

func TestParseSeedBad(t *testing.T) {
	if _, err := ParseSeed("=novalue"); !errors.Is(err, ErrBadSeed) {
		t.Fatalf("ParseSeed bad = %v, want ErrBadSeed", err)
	}
}

func TestSeedSkipsEmptyPairs(t *testing.T) {
	seed, err := ParseSeed("Hash=ABCDEFGHIJKL,,Port=8090")
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) != 2 {
		t.Errorf("expected 2 fields, got %d", len(seed))
	}
}

func TestSeedPortInvalid(t *testing.T) {
	seed := Seed{SeedPort: "notnum"}
	if _, err := seed.Port(); !errors.Is(err, ErrBadSeed) {
		t.Fatalf("Port() = %v, want ErrBadSeed", err)
	}
}
