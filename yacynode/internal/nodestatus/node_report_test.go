package nodestatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
)

type stubCounter struct {
	rwi     int
	rwiURLs int
	urls    int
	err     error
}

func (c stubCounter) RWICount(context.Context) (int, error) { return c.rwi, c.err }

func (c stubCounter) RWIURLCount(context.Context, yacymodel.Hash) (int, error) {
	return c.rwiURLs, c.err
}

func (c stubCounter) Count(context.Context) (int, error) { return c.urls, c.err }

type stubPeers struct {
	known int
}

func (p stubPeers) KnownPeerCount(context.Context) int { return p.known }

type stubNews struct {
	attachment string
}

func (n stubNews) SeedNews(context.Context) string { return n.attachment }

type stubTransfers struct {
	totals TransferTotals
}

func (s stubTransfers) TransferTotals(context.Context) TransferTotals { return s.totals }

func testIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{
		Hash:        yacymodel.WordHash("self"),
		NetworkName: "freeworld",
		Name:        "node",
		Host:        "192.0.2.1",
		Port:        8090,
		Flags:       yacymodel.ZeroFlags(),
		Version:     "1.2",
	}
}

func clockAt(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func reportAt(start time.Time, elapsed time.Duration, rwi, urls stubCounter) nodeReport {
	id := testIdentity()
	id.Start = start

	return newReport(id, clockAt(start.Add(elapsed)), ReportSources{
		RWI:   rwi,
		URLs:  urls,
		Peers: stubPeers{known: 4},
		News:  stubNews{attachment: "b|news"},
		Transfers: stubTransfers{totals: TransferTotals{
			SentWords:     11,
			ReceivedWords: 22,
			SentURLs:      33,
			ReceivedURLs:  44,
		}},
	})
}

func TestSelfSeedRefreshesDynamicFields(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	counts := stubCounter{rwi: 7, urls: 3}
	report := reportAt(start, 90*time.Minute, counts, counts)

	seed := report.SelfSeed(context.Background())

	if got, _ := seed.Uptime.Get(); got != 90 {
		t.Fatalf("Uptime = %d, want 90", got)
	}
	if got, _ := seed.RWICount.Get(); got != 7 {
		t.Fatalf("RWICount = %d, want 7", got)
	}
	if got, _ := seed.URLCount.Get(); got != 3 {
		t.Fatalf("URLCount = %d, want 3", got)
	}
	if _, ok := seed.LastSeen.Get(); !ok {
		t.Fatal("LastSeen unset")
	}
	if _, ok := seed.UTC.Get(); !ok {
		t.Fatal("UTC unset")
	}
	if got, _ := seed.KnownSeedCount.Get(); got != 4 {
		t.Fatalf("KnownSeedCount = %d, want 4", got)
	}
	if got, ok := seed.News.Get(); !ok || got != "b|news" {
		t.Fatalf("News = %q (set %v), want current attachment", got, ok)
	}
	for name, want := range map[string]struct {
		field yacymodel.Optional[int64]
		value int64
	}{
		yacymodel.SeedSentWordCount:     {seed.SentWordCount, 11},
		yacymodel.SeedReceivedWordCount: {seed.ReceivedWordCount, 22},
		yacymodel.SeedSentURLCount:      {seed.SentURLCount, 33},
		yacymodel.SeedReceivedURLCount:  {seed.ReceivedURLCount, 44},
	} {
		got, ok := want.field.Get()
		if !ok || got != want.value {
			t.Fatalf("%s = %d (set %v), want %d", name, got, ok, want.value)
		}
	}
	for name, field := range map[string]yacymodel.Optional[int]{
		yacymodel.SeedNoticedURLCount: seed.NoticedURLCount,
		yacymodel.SeedOfferedURLCount: seed.OfferedURLCount,
		yacymodel.SeedConnectsPerHour: seed.ConnectsPerHour,
		yacymodel.SeedIndexingSpeed:   seed.IndexingSpeed,
		yacymodel.SeedRequestSpeed:    seed.RequestSpeed,
		yacymodel.SeedUplinkSpeed:     seed.UplinkSpeed,
	} {
		got, ok := field.Get()
		if !ok || got != 0 {
			t.Fatalf("%s = %d (set %v), want reported 0", name, got, ok)
		}
	}
}

func TestSelfSeedCarriesPersistentBirthDate(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	birth := time.Date(2025, time.January, 5, 12, 0, 0, 0, time.UTC)
	id := testIdentity()
	id.Start = start
	id.BirthDate = birth
	report := newReport(id, clockAt(start), ReportSources{
		RWI:       stubCounter{},
		URLs:      stubCounter{},
		Peers:     stubPeers{},
		News:      stubNews{},
		Transfers: stubTransfers{},
	})

	seed := report.SelfSeed(context.Background())

	got, ok := seed.BirthDate.Get()
	if !ok || !got.Time().Equal(birth) {
		t.Fatalf("BirthDate = %v (set %v), want %v", got, ok, birth)
	}
}

func TestSelfSeedKeepsIdentityFields(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	counts := stubCounter{}
	report := reportAt(start, 0, counts, counts)

	seed := report.SelfSeed(context.Background())

	if seed.Hash != yacymodel.WordHash("self") {
		t.Fatalf("Hash = %q, want self hash", seed.Hash)
	}
	if name, _ := seed.Name.Get(); name != "node" {
		t.Fatalf("Name = %q, want node", name)
	}
	if port, _ := seed.Port.Get(); port != yacymodel.Port(8090) {
		t.Fatalf("Port = %d, want 8090", port)
	}
	if peerType, _ := seed.PeerType.Get(); peerType != yacymodel.PeerSenior {
		t.Fatalf("PeerType = %q, want senior", peerType)
	}
	host, ok := seed.IP.Get()
	if !ok || host.String() != "192.0.2.1" {
		t.Fatalf("IP = %q (set %v), want 192.0.2.1", host, ok)
	}
	if _, ok := seed.BirthDate.Get(); ok {
		t.Fatal("BirthDate set without a persistent identity birth date")
	}
}

func TestSelfSeedCountErrorsReportZero(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	counts := stubCounter{rwi: 5, urls: 9, err: errors.New("boom")}
	report := reportAt(start, time.Hour, counts, counts)

	seed := report.SelfSeed(context.Background())

	if got, _ := seed.RWICount.Get(); got != 0 {
		t.Fatalf("RWICount = %d, want 0 on error", got)
	}
	if got, _ := seed.URLCount.Get(); got != 0 {
		t.Fatalf("URLCount = %d, want 0 on error", got)
	}
}

func TestHeaderReportsVersionAndUptime(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	report := reportAt(start, 45*time.Minute, stubCounter{}, stubCounter{})

	ctx := context.Background()

	if got := report.Version(ctx); got != "1.2" {
		t.Fatalf("Version = %q, want 1.2", got)
	}
	if got := report.Uptime(ctx); got != 45 {
		t.Fatalf("Uptime = %d, want 45", got)
	}
}

func TestNewReportReturnsRuntimeReport(t *testing.T) {
	id := testIdentity()
	id.Start = time.Now().Add(-time.Minute)
	report := NewReport(id, ReportSources{
		RWI:       stubCounter{},
		URLs:      stubCounter{},
		Peers:     stubPeers{},
		News:      stubNews{},
		Transfers: stubTransfers{},
	})

	if got := report.Version(context.Background()); got != "1.2" {
		t.Fatalf("Version = %q, want 1.2", got)
	}
}
