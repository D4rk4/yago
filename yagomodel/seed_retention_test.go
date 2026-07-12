package yagomodel

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseSeedBoundsPlainFormAndProperties(t *testing.T) {
	exact := "Hash=ABCDEFGHIJKL" +
		",a=" + strings.Repeat("a", maximumSeedPropertyValue) +
		",b=" + strings.Repeat("b", maximumSeedPropertyValue) +
		",c=" + strings.Repeat("c", maximumSeedPropertyValue) +
		",d=" + strings.Repeat("d", 8163)
	if len(exact) != maximumSeedPlainBytes {
		t.Fatalf("exact seed bytes = %d, want %d", len(exact), maximumSeedPlainBytes)
	}
	if _, err := ParseSeed(t.Context(), exact); err != nil {
		t.Fatalf("ParseSeed exact plain bound: %v", err)
	}
	if _, err := ParseSeed(t.Context(), exact+"x"); !errors.Is(err, ErrBadSeed) {
		t.Fatalf("ParseSeed above plain bound = %v, want ErrBadSeed", err)
	}

	properties := make([]string, maximumSeedProperties, maximumSeedProperties+1)
	properties[0] = "Hash=ABCDEFGHIJKL"
	for index := 1; index < len(properties); index++ {
		properties[index] = fmt.Sprintf("p%d=", index)
	}
	if _, err := ParseSeed(t.Context(), strings.Join(properties, ",")); err != nil {
		t.Fatalf("ParseSeed exact property bound: %v", err)
	}
	properties = append(properties, "overflow=")
	if _, err := ParseSeed(
		t.Context(),
		strings.Join(properties, ","),
	); !errors.Is(err, ErrBadSeed) {
		t.Fatalf("ParseSeed above property bound = %v, want ErrBadSeed", err)
	}
}

func TestParseSeedBoundsKeysAndValues(t *testing.T) {
	exactKey := strings.Repeat("k", maximumSeedPropertyKey)
	if _, err := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,"+exactKey+"=value"); err != nil {
		t.Fatalf("ParseSeed exact key bound: %v", err)
	}
	if _, err := ParseSeed(
		t.Context(),
		"Hash=ABCDEFGHIJKL,"+exactKey+"k=value",
	); !errors.Is(
		err,
		ErrBadSeed,
	) {
		t.Fatalf("ParseSeed above key bound = %v, want ErrBadSeed", err)
	}

	cases := []struct {
		name  string
		key   string
		limit int
	}{
		{name: "generic", key: "Custom", limit: maximumSeedPropertyValue},
		{name: "name", key: SeedName, limit: maximumSeedNameBytes},
		{name: "news", key: SeedNews, limit: maximumSeedNewsBytes},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			exact := "Hash=ABCDEFGHIJKL," + test.key + "=" + strings.Repeat("v", test.limit)
			if _, err := ParseSeed(t.Context(), exact); err != nil {
				t.Fatalf("ParseSeed exact %s value bound: %v", test.key, err)
			}
			if _, err := ParseSeed(t.Context(), exact+"v"); !errors.Is(err, ErrBadSeed) {
				t.Fatalf("ParseSeed above %s value bound = %v, want ErrBadSeed", test.key, err)
			}
		})
	}
}

func TestParseSeedWireFormBoundsCompressedInflation(t *testing.T) {
	payload := "Hash=ABCDEFGHIJKL,Custom=" + strings.Repeat("x", maximumSeedPlainBytes)
	encoded := EncodeCompactWireForm(payload)
	if len(encoded) >= len(payload) {
		t.Fatalf("compact seed bytes = %d, want less than %d", len(encoded), len(payload))
	}
	if _, err := ParseSeedWireForm(
		t.Context(),
		encoded,
	); !errors.Is(
		err,
		errInflatedWireFormTooLarge,
	) {
		t.Fatalf("ParseSeedWireForm compact bomb = %v, want inflated bound error", err)
	}
}

func TestParseSeedWireFormAcceptsOrdinaryEncodings(t *testing.T) {
	forms := []string{
		sampleSeed,
		EncodeBase64WireForm(sampleSeed),
		EncodeCompactWireForm(sampleSeed),
	}
	for _, form := range forms {
		seed, err := ParseSeedWireForm(t.Context(), form)
		if err != nil {
			t.Fatalf("ParseSeedWireForm(%q): %v", form, err)
		}
		if seed.String() != sampleSeed {
			t.Fatalf("seed round trip = %q, want %q", seed.String(), sampleSeed)
		}
	}
}

func TestParseSeedRetainsCompactOwnedStrings(t *testing.T) {
	plain := "Hash=ABCDEFGHIJKL,Name=peer-name,IP=peer.example,PeerType=senior," +
		"Version=1.83,UTC=+0230,Custom=custom-value"
	prefix := strings.Repeat("p", 1<<20)
	backing := prefix + plain + strings.Repeat("s", 1<<20)
	view := backing[len(prefix) : len(prefix)+len(plain)]
	seed, err := ParseSeed(t.Context(), view)
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	assertStringOutsideBacking(t, backing, seed.Hash.String())
	name, _ := seed.Name.Get()
	assertStringOutsideBacking(t, backing, name)
	host, _ := seed.IP.Get()
	assertStringOutsideBacking(t, backing, host.String())
	peerType, _ := seed.PeerType.Get()
	assertStringOutsideBacking(t, backing, peerType.String())
	version, _ := seed.Version.Get()
	assertStringOutsideBacking(t, backing, version.String())
	utc, _ := seed.UTC.Get()
	assertStringOutsideBacking(t, backing, utc.String())
	for key, value := range seed.customProperties {
		assertStringOutsideBacking(t, backing, key)
		assertStringOutsideBacking(t, backing, value)
	}
}

func TestSeedCopyOwnsMutableAndStringData(t *testing.T) {
	seed, err := ParseSeed(
		t.Context(),
		"Hash=ABCDEFGHIJKL,Name=peer-name,IP=peer.example,IP6=2001:db8::1,"+
			"PeerType=senior,Version=1.83,UTC=+0230,Custom=custom-value",
	)
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}
	seed.News = Some("bulletin")
	copied := seed.Copy()

	assertDistinctString(t, seed.Hash.String(), copied.Hash.String())
	seedName, _ := seed.Name.Get()
	copiedName, _ := copied.Name.Get()
	assertDistinctString(t, seedName, copiedName)
	seedHost, _ := seed.IP.Get()
	copiedHost, _ := copied.IP.Get()
	assertDistinctString(t, seedHost.String(), copiedHost.String())
	seedPeerType, _ := seed.PeerType.Get()
	copiedPeerType, _ := copied.PeerType.Get()
	assertDistinctString(t, seedPeerType.String(), copiedPeerType.String())
	seedVersion, _ := seed.Version.Get()
	copiedVersion, _ := copied.Version.Get()
	assertDistinctString(t, seedVersion.String(), copiedVersion.String())
	seedUTC, _ := seed.UTC.Get()
	copiedUTC, _ := copied.UTC.Get()
	assertDistinctString(t, seedUTC.String(), copiedUTC.String())
	seedNews, _ := seed.News.Get()
	copiedNews, _ := copied.News.Get()
	assertDistinctString(t, seedNews, copiedNews)
	assertDistinctString(t, seed.customProperties["Custom"], copied.customProperties["Custom"])

	seedHosts, _ := seed.IP6.Get()
	copiedHosts, _ := copied.IP6.Get()
	if &seedHosts[0] == &copiedHosts[0] {
		t.Fatal("Seed.Copy retained the IP6 slice")
	}
	if copied.RetainedBytes() != seed.RetainedBytes() {
		t.Fatal("equivalent seeds reported different retained bytes")
	}
	seedHosts[0] = Host{}
	seed.customProperties["Custom"] = "changed"
	if copiedHosts[0].String() != "2001:db8::1" {
		t.Fatalf("copied IP6 = %q, want original address", copiedHosts[0])
	}
	if copied.customProperties["Custom"] != "custom-value" {
		t.Fatalf("copied custom value = %q", copied.customProperties["Custom"])
	}
}

func TestEmptySeedCopyAndRetention(t *testing.T) {
	empty := Seed{}
	if copied := empty.Copy(); !reflect.DeepEqual(copied, empty) {
		t.Fatalf("empty copy = %#v", copied)
	}
	if retained := empty.RetainedBytes(); retained != seedRetentionWidth {
		t.Fatalf("empty retained bytes = %d, want %d", retained, seedRetentionWidth)
	}
}

func assertStringOutsideBacking(t *testing.T, backing, retained string) {
	t.Helper()
	backingStart := reflect.ValueOf(backing).Pointer()
	backingEnd := backingStart + uintptr(len(backing))
	retainedStart := reflect.ValueOf(retained).Pointer()
	if retainedStart >= backingStart && retainedStart < backingEnd {
		t.Fatalf("retained string %q aliases source backing", retained)
	}
}

func assertDistinctString(t *testing.T, source, copied string) {
	t.Helper()
	if source != copied {
		t.Fatalf("copied string = %q, want %q", copied, source)
	}
	if reflect.ValueOf(source).Pointer() == reflect.ValueOf(copied).Pointer() {
		t.Fatalf("copied string %q retained source storage", copied)
	}
}
