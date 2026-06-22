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

func TestSeedTrafficFieldsRoundTrip(t *testing.T) {
	seed, err := ParseSeed(
		t.Context(),
		"{Hash=ABCDEFGHIJKL,sI=1,rI=2,sU=3,rU=4,USpeed=5,BDate=20260622012208,ISpeed=6,RSpeed=7.5,NCount=8,RCount=9,SCount=10,CCount=11.25}",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := seed.String(); got != "{BDate=20260622012208,CCount=11.25,Hash=ABCDEFGHIJKL,ISpeed=6,NCount=8,RCount=9,RSpeed=7.5,SCount=10,USpeed=5,rI=2,rU=4,sI=1,sU=3}" {
		t.Errorf("round trip:\n got %q", got)
	}
	assertOptional(t, "IndexOut", seed.IndexOut, int64(1))
	assertOptional(t, "IndexIn", seed.IndexIn, int64(2))
	assertOptional(t, "URLOut", seed.URLOut, int64(3))
	assertOptional(t, "URLIn", seed.URLIn, int64(4))
	assertOptional(t, "UploadSpeed", seed.UploadSpeed, int64(5))
	assertOptional(t, "BirthDate", seed.BirthDate, "20260622012208")
	assertOptional(t, "IndexSpeed", seed.IndexSpeed, int64(6))
	assertOptional(t, "RetrievalSpeed", seed.RetrievalSpeed, 7.5)
	assertOptional(t, "NoticedURLCount", seed.NoticedURLCount, int64(8))
	assertOptional(t, "RemoteCrawlURLCount", seed.RemoteCrawlURLCount, int64(9))
	assertOptional(t, "SeedCount", seed.SeedCount, int64(10))
	assertOptional(t, "ClientConnectCount", seed.ClientConnectCount, 11.25)
}

func assertOptional[T comparable](t *testing.T, name string, got Optional[T], want T) {
	t.Helper()
	value, ok := got.Get()
	if !ok || value != want {
		t.Errorf("%s = %v, %v", name, value, ok)
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

func TestParseSeedIP6List(t *testing.T) {
	value := "2001:db8::1|2001:db8::2"
	seed, err := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,IP6="+value)
	if err != nil {
		t.Fatal(err)
	}
	if ip6, ok := seed.IP6.Get(); !ok || ip6.String() != value {
		t.Fatalf("IP6 = %q, %v", ip6, ok)
	}
	if got := seed.String(); got != "{Hash=ABCDEFGHIJKL,IP6="+value+"}" {
		t.Fatalf("round trip = %q", got)
	}
}
