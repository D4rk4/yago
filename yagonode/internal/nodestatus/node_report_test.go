package nodestatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
)

type stubCounter struct {
	rwi     int
	rwiURLs int
	urls    int
	err     error
}

func (c stubCounter) RWICount(context.Context) (int, error) { return c.rwi, c.err }

func (c stubCounter) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return c.rwiURLs, c.err
}

func (c stubCounter) Count(context.Context) (int, error) { return c.urls, c.err }

type stubPeers struct {
	reachable int
}

func (p stubPeers) ReachablePeerCount(context.Context) int { return p.reachable }

type stubSeedQueues struct {
	statistics SeedQueueStatistics
}

func (s stubSeedQueues) SeedQueueStatistics(context.Context) SeedQueueStatistics {
	return s.statistics
}

type stubNews struct {
	attachment string
}

func (n stubNews) SeedNews(context.Context) string { return n.attachment }

type stubTransfers struct {
	totals TransferTotals
}

func (s stubTransfers) TransferTotals(context.Context) TransferTotals { return s.totals }

type stubPublishedPeerClassification struct {
	peerType yagomodel.PeerType
}

func (s stubPublishedPeerClassification) PublishedPeerType(context.Context) yagomodel.PeerType {
	return s.peerType
}

func testIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{
		Hash:        yagomodel.WordHash("self"),
		NetworkName: "freeworld",
		Name:        "node",
		Host:        "192.0.2.1",
		Port:        8090,
		Flags:       yagomodel.ZeroFlags(),
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
		Peers: stubPeers{reachable: 4},
		Queues: stubSeedQueues{statistics: SeedQueueStatistics{
			Noticed: 9, NoticedKnown: true, Offered: 6, OfferedKnown: true,
		}},
		News: stubNews{attachment: "b|news"},
		Transfers: stubTransfers{totals: TransferTotals{
			Known:         true,
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
		field yagomodel.Optional[int64]
		value int64
	}{
		yagomodel.SeedSentWordCount:     {seed.SentWordCount, 11},
		yagomodel.SeedReceivedWordCount: {seed.ReceivedWordCount, 22},
		yagomodel.SeedSentURLCount:      {seed.SentURLCount, 33},
		yagomodel.SeedReceivedURLCount:  {seed.ReceivedURLCount, 44},
	} {
		got, ok := want.field.Get()
		if !ok || got != want.value {
			t.Fatalf("%s = %d (set %v), want %d", name, got, ok, want.value)
		}
	}
	for name, want := range map[string]struct {
		field yagomodel.Optional[int]
		value int
	}{
		yagomodel.SeedNoticedURLCount: {seed.NoticedURLCount, 9},
		yagomodel.SeedOfferedURLCount: {seed.OfferedURLCount, 6},
	} {
		got, ok := want.field.Get()
		if !ok || got != want.value {
			t.Fatalf("%s = %d (set %v), want %d", name, got, ok, want.value)
		}
	}
	for name, field := range map[string]yagomodel.Optional[int]{
		yagomodel.SeedConnectsPerHour: seed.ConnectsPerHour,
		yagomodel.SeedIndexingSpeed:   seed.IndexingSpeed,
		yagomodel.SeedRequestSpeed:    seed.RequestSpeed,
		yagomodel.SeedUplinkSpeed:     seed.UplinkSpeed,
	} {
		if _, ok := field.Get(); ok {
			t.Fatalf("%s was reported without a measurement", name)
		}
	}
}

func TestSelfSeedPublishesReachablePeerCount(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	id := testIdentity()
	id.Start = start
	report := newReport(id, clockAt(start), ReportSources{
		RWI:       stubCounter{},
		URLs:      stubCounter{},
		Peers:     stubPeers{reachable: 7},
		News:      stubNews{},
		Transfers: stubTransfers{},
	})

	if got, known := report.SelfSeed(t.Context()).KnownSeedCount.Get(); got != 7 || !known {
		t.Fatalf("KnownSeedCount = %d known=%t, want 7 true", got, known)
	}
}

func TestSelfSeedKeepsUnavailableQueueStatisticsUnknown(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	id := testIdentity()
	id.Start = start
	report := newReport(id, clockAt(start), ReportSources{
		RWI:       stubCounter{},
		URLs:      stubCounter{},
		Peers:     stubPeers{},
		Queues:    stubSeedQueues{},
		News:      stubNews{},
		Transfers: stubTransfers{},
	})

	seed := report.SelfSeed(t.Context())
	if _, known := seed.NoticedURLCount.Get(); known {
		t.Fatal("unavailable noticed queue depth was published")
	}
	if _, known := seed.OfferedURLCount.Get(); known {
		t.Fatal("unavailable offered queue depth was published")
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

	if seed.Hash != yagomodel.WordHash("self") {
		t.Fatalf("Hash = %q, want self hash", seed.Hash)
	}
	if name, _ := seed.Name.Get(); name != "node" {
		t.Fatalf("Name = %q, want node", name)
	}
	if port, _ := seed.Port.Get(); port != yagomodel.Port(8090) {
		t.Fatalf("Port = %d, want 8090", port)
	}
	if peerType, _ := seed.PeerType.Get(); peerType != yagomodel.PeerVirgin {
		t.Fatalf("PeerType = %q, want virgin", peerType)
	}
	host, ok := seed.IP.Get()
	if !ok || host.String() != "192.0.2.1" {
		t.Fatalf("IP = %q (set %v), want 192.0.2.1", host, ok)
	}
	if _, ok := seed.BirthDate.Get(); ok {
		t.Fatal("BirthDate set without a persistent identity birth date")
	}
}

func TestSelfSeedPublishesRuntimePeerClassification(t *testing.T) {
	for _, test := range []struct {
		peerType yagomodel.PeerType
		want     yagomodel.PeerType
	}{
		{peerType: yagomodel.PeerVirgin, want: yagomodel.PeerVirgin},
		{peerType: yagomodel.PeerJunior, want: yagomodel.PeerJunior},
		{peerType: yagomodel.PeerSenior, want: yagomodel.PeerSenior},
		{peerType: yagomodel.PeerPrincipal, want: yagomodel.PeerPrincipal},
		{peerType: yagomodel.PeerMentor, want: yagomodel.PeerVirgin},
	} {
		t.Run(test.peerType.String(), func(t *testing.T) {
			start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
			id := testIdentity()
			id.Start = start
			report := newReport(id, clockAt(start), ReportSources{
				RWI:       stubCounter{},
				URLs:      stubCounter{},
				Peers:     stubPeers{},
				News:      stubNews{},
				Transfers: stubTransfers{},
				PeerClassification: stubPublishedPeerClassification{
					peerType: test.peerType,
				},
			})

			got, known := report.SelfSeed(t.Context()).PeerType.Get()
			if !known || got != test.want || report.PublishedPeerType(t.Context()) != test.want {
				t.Fatalf("PeerType = %q known=%t, want %q true", got, known, test.want)
			}
		})
	}
}

func TestSelfSeedCountErrorsRemainUnavailable(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	counts := stubCounter{rwi: 5, urls: 9, err: errors.New("boom")}
	report := reportAt(start, time.Hour, counts, counts)

	seed := report.SelfSeed(context.Background())

	if got, known := seed.RWICount.Get(); got != 0 || known {
		t.Fatalf("RWICount = %d known=%t, want unavailable on error", got, known)
	}
	if got, known := seed.URLCount.Get(); got != 0 || known {
		t.Fatalf("URLCount = %d known=%t, want unavailable on error", got, known)
	}
}

func TestSelfSeedNegativeCountsClampToZero(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	counts := stubCounter{rwi: -5, urls: -9}
	seed := reportAt(start, time.Hour, counts, counts).SelfSeed(context.Background())

	if got, known := seed.RWICount.Get(); got != 0 || !known {
		t.Fatalf("RWICount = %d known=%t, want 0 true", got, known)
	}
	if got, known := seed.URLCount.Get(); got != 0 || !known {
		t.Fatalf("URLCount = %d known=%t, want 0 true", got, known)
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
	if got := report.UptimeSeconds(ctx); got != 45*60 {
		t.Fatalf("UptimeSeconds = %d, want %d", got, 45*60)
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
