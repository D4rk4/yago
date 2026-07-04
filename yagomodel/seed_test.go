package yagomodel

import (
	"errors"
	"testing"
	"time"
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

func TestSeedDomainFieldsRoundTrip(t *testing.T) {
	seed, err := ParseSeed(
		t.Context(),
		"{BDate=20260621012208,Hash=ABCDEFGHIJKL,LastSeen=20260622012208,UTC=+0230,Version=1.83}",
	)
	if err != nil {
		t.Fatal(err)
	}
	version, ok := seed.Version.Get()
	if !ok || version.String() != "1.83" {
		t.Errorf("Version = %q, %v", version, ok)
	}
	utc, ok := seed.UTC.Get()
	if !ok || utc.String() != "+0230" {
		t.Errorf("UTC = %q, %v", utc, ok)
	}
	lastSeen, ok := seed.LastSeen.Get()
	if !ok || !lastSeen.Time().Equal(time.Date(2026, 6, 22, 1, 22, 8, 0, time.UTC)) {
		t.Errorf("LastSeen = %q, %v", lastSeen, ok)
	}
	birthDate, ok := seed.BirthDate.Get()
	if !ok || !birthDate.Time().Equal(time.Date(2026, 6, 21, 1, 22, 8, 0, time.UTC)) {
		t.Errorf("BirthDate = %q, %v", birthDate, ok)
	}
	if got := seed.String(); got != "{BDate=20260621012208,Hash=ABCDEFGHIJKL,LastSeen=20260622012208,UTC=+0230,Version=1.83}" {
		t.Errorf("round trip:\n got %q", got)
	}
}

func TestSeedAcceptsTimestampUTCField(t *testing.T) {
	seed, err := ParseSeed(
		t.Context(),
		"{Hash=ABCDEFGHIJKL,LastSeen=20260622012208,UTC=20260614000329}",
	)
	if err != nil {
		t.Fatal(err)
	}
	utc, ok := seed.UTC.Get()
	if !ok || utc.String() != "20260614000329" {
		t.Fatalf("UTC = %q, %v", utc, ok)
	}
	if got := seed.String(); got != "{Hash=ABCDEFGHIJKL,LastSeen=20260622012208,UTC=20260614000329}" {
		t.Fatalf("seed string = %q", got)
	}
}

func TestSeedOperationalFieldsRoundTrip(t *testing.T) {
	seed, err := ParseSeed(
		t.Context(),
		"{Hash=ABCDEFGHIJKL,PortSSL=8443,Uptime=12,ICount=34,LCount=56}",
	)
	if err != nil {
		t.Fatal(err)
	}
	if port, ok := seed.PortSSL.Get(); !ok || port.String() != "8443" {
		t.Fatalf("PortSSL = %q, %v", port, ok)
	}
	if uptime, ok := seed.Uptime.Get(); !ok || uptime != 12 {
		t.Fatalf("Uptime = %d, %v", uptime, ok)
	}
	if count, ok := seed.RWICount.Get(); !ok || count != 34 {
		t.Fatalf("ICount = %d, %v", count, ok)
	}
	if count, ok := seed.URLCount.Get(); !ok || count != 56 {
		t.Fatalf("LCount = %d, %v", count, ok)
	}
	if got := seed.String(); got != "{Hash=ABCDEFGHIJKL,ICount=34,LCount=56,PortSSL=8443,Uptime=12}" {
		t.Fatalf("seed string = %q", got)
	}
}

func TestSeedSharesAddress(t *testing.T) {
	left := seedWithAddress(t, "AAAAAAAAAAAA", "192.0.2.1", "2001:db8::1")
	if !left.SharesAddress(seedWithAddress(t, "BBBBBBBBBBBB", "192.0.2.1", "")) {
		t.Fatal("expected IPv4 address match")
	}
	if !left.SharesAddress(seedWithAddress(t, "BBBBBBBBBBBB", "", "2001:db8::1")) {
		t.Fatal("expected IPv6 address match")
	}
	if left.SharesAddress(seedWithAddress(t, "BBBBBBBBBBBB", "192.0.2.2", "2001:db8::2")) {
		t.Fatal("different addresses should not match")
	}
	if (Seed{}).SharesAddress(left) {
		t.Fatal("empty seed should not match")
	}
}

func TestParseSeedRejectsBadUTC(t *testing.T) {
	if _, err := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,UTC=0200"); !errors.Is(err, ErrBadSeed) {
		t.Fatalf("ParseSeed bad utc = %v, want ErrBadSeed", err)
	}
}

func seedWithAddress(t *testing.T, hash, ip, ip6 string) Seed {
	t.Helper()

	seed := Seed{Hash: Hash(hash)}
	if ip != "" {
		host, err := ParseHost(ip)
		if err != nil {
			t.Fatalf("ParseHost: %v", err)
		}
		seed.IP = Some(host)
	}
	if ip6 != "" {
		hosts, err := ParseIP6(ip6)
		if err != nil {
			t.Fatalf("ParseIP6: %v", err)
		}
		seed.IP6 = Some(hosts)
	}

	return seed
}

func TestParseSeedRejectsBadLastSeen(t *testing.T) {
	if _, err := ParseSeed(
		t.Context(),
		"Hash=ABCDEFGHIJKL,LastSeen=2026-06-22",
	); !errors.Is(
		err,
		ErrBadSeed,
	) {
		t.Fatalf("ParseSeed bad last seen = %v, want ErrBadSeed", err)
	}
}

func TestParseSeedRejectsBadBirthDate(t *testing.T) {
	if _, err := ParseSeed(
		t.Context(),
		"Hash=ABCDEFGHIJKL,BDate=2026-06-22",
	); !errors.Is(
		err,
		ErrBadSeed,
	) {
		t.Fatalf("ParseSeed bad birth date = %v, want ErrBadSeed", err)
	}
}

func TestSeedAgeDaysUsesBirthDate(t *testing.T) {
	birthDate := NewSeedBirthDateUTC(time.Date(2026, 6, 20, 12, 0, 0, 900, time.UTC))
	seed := Seed{Hash: "ABCDEFGHIJKL", BirthDate: Some(birthDate)}
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

	if got := seed.AgeDays(now); got != 3 {
		t.Fatalf("AgeDays = %d, want 3", got)
	}
}

func TestSeedAgeDaysDefaultsToLegacyYaCyBirthDate(t *testing.T) {
	seed := Seed{Hash: "ABCDEFGHIJKL"}
	now := time.Date(2004, 1, 4, 0, 0, 0, 0, time.UTC)

	if got := seed.AgeDays(now); got != 3 {
		t.Fatalf("AgeDays default = %d, want 3", got)
	}
}

func TestSeedAgeDaysMatchesYaCyAbsoluteDelta(t *testing.T) {
	birthDate := NewSeedBirthDateUTC(time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC))
	seed := Seed{Hash: "ABCDEFGHIJKL", BirthDate: Some(birthDate)}
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	if got := seed.AgeDays(now); got != 4 {
		t.Fatalf("AgeDays future birth date = %d, want 4", got)
	}
}

func TestSeedLegacyPropertiesRoundTrip(t *testing.T) {
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
}

func TestSeedCustomPropertiesRoundTrip(t *testing.T) {
	seed, err := ParseSeed(
		t.Context(),
		"{Hash=ABCDEFGHIJKL,Country=DE,CRTCnt=2,CRWCnt=3,dct=4,IPType=ipv4,JRE=21,news=hello,seedURL=https://example.org/seed.txt,SorlAvail=OK,Tags=foo|bar}",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := seed.String(); got != "{CRTCnt=2,CRWCnt=3,Country=DE,Hash=ABCDEFGHIJKL,IPType=ipv4,JRE=21,SorlAvail=OK,Tags=foo|bar,dct=4,news=hello,seedURL=https://example.org/seed.txt}" {
		t.Fatalf("round trip:\n got %q", got)
	}
	if got := seed.customProperties["Country"]; got != "DE" {
		t.Fatalf("Country = %q", got)
	}
	if got := seed.customProperties["CRTCnt"]; got != "2" {
		t.Fatalf("CRTCnt = %q", got)
	}
	if got := seed.customProperties["CRWCnt"]; got != "3" {
		t.Fatalf("CRWCnt = %q", got)
	}
	if got := seed.customProperties["dct"]; got != "4" {
		t.Fatalf("dct = %q", got)
	}
	if got := seed.customProperties["IPType"]; got != "ipv4" {
		t.Fatalf("IPType = %q", got)
	}
	if got := seed.customProperties["JRE"]; got != "21" {
		t.Fatalf("JRE = %q", got)
	}
	if got := seed.customProperties["news"]; got != "hello" {
		t.Fatalf("news = %q", got)
	}
	if got := seed.customProperties["seedURL"]; got != "https://example.org/seed.txt" {
		t.Fatalf("seedURL = %q", got)
	}
	if got := seed.customProperties["SorlAvail"]; got != "OK" {
		t.Fatalf("SorlAvail = %q", got)
	}
	if got := seed.customProperties["Tags"]; got != "foo|bar" {
		t.Fatalf("Tags = %q", got)
	}
}

func TestSeedPropertiesIncludeCoreAndCustomValues(t *testing.T) {
	seed, err := ParseSeed(
		t.Context(),
		"{Hash=ABCDEFGHIJKL,IP=192.0.2.1,Port=8090,Country=DE}",
	)
	if err != nil {
		t.Fatal(err)
	}

	props := seed.Properties()
	if got := props[SeedHash]; got != "ABCDEFGHIJKL" {
		t.Fatalf("Hash = %q", got)
	}
	if got := props[SeedIP]; got != "192.0.2.1" {
		t.Fatalf("IP = %q", got)
	}
	if got := props[SeedPort]; got != "8090" {
		t.Fatalf("Port = %q", got)
	}
	if got := props["Country"]; got != "DE" {
		t.Fatalf("Country = %q", got)
	}
	props[SeedHash] = "changed"
	if seed.Hash != "ABCDEFGHIJKL" {
		t.Fatalf("seed Hash changed to %q", seed.Hash)
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
	seed, err := ParseSeed(t.Context(), "{Hash=ABCDEFGHIJKL,   ,Port=8090}")
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
	if ip6, ok := seed.IP6.Get(); !ok || len(ip6) != 2 || ip6[0].String() != "2001:db8::1" ||
		ip6[1].String() != "2001:db8::2" {
		t.Fatalf("IP6 = %q, %v", ip6, ok)
	}
	if got := seed.String(); got != "{Hash=ABCDEFGHIJKL,IP6="+value+"}" {
		t.Fatalf("round trip = %q", got)
	}
}

func TestParseSeedIP6AcceptsIPv4(t *testing.T) {
	seed, err := ParseSeed(t.Context(), "Hash=ABCDEFGHIJKL,IP6=192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if ip6, ok := seed.IP6.Get(); !ok || len(ip6) != 1 || ip6[0].String() != "192.0.2.1" {
		t.Fatalf("IP6 = %q, %v", ip6, ok)
	}
}

func TestParseSeedIP6RejectsEmptySegment(t *testing.T) {
	if _, err := ParseSeed(
		t.Context(),
		"Hash=ABCDEFGHIJKL,IP6=2001:db8::1|",
	); !errors.Is(
		err,
		ErrBadSeed,
	) {
		t.Fatalf("ParseSeed bad ip6 = %v, want ErrBadSeed", err)
	}
}
