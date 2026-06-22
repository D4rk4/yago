package nodestatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type stubCounter struct {
	rwi  int
	refs int
	urls int
	err  error
}

func (c stubCounter) RWICount(context.Context) (int, error)           { return c.rwi, c.err }
func (c stubCounter) ReferencedURLCount(context.Context) (int, error) { return c.refs, c.err }
func (c stubCounter) Count(context.Context) (int, error)              { return c.urls, c.err }

func testIdentity() Identity {
	return Identity{
		Hash:        yacymodel.WordHash("self"),
		NetworkName: "freeworld",
		Name:        "node",
		Host:        "192.0.2.1",
		Port:        8090,
		Flags:       yacymodel.ZeroFlags(),
		Version:     "1.2",
	}
}

func fixedClock(start time.Time, elapsed time.Duration) func() time.Time {
	calls := 0
	return func() time.Time {
		defer func() { calls++ }()
		if calls == 0 {
			return start
		}
		return start.Add(elapsed)
	}
}

func fixedLiveness(version string, start time.Time, elapsed time.Duration) Liveness {
	now := fixedClock(start, elapsed)
	return Liveness{version: version, start: now(), now: now}
}

func TestSelfSeedRefreshesDynamicFields(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	counts := stubCounter{rwi: 7, urls: 3}
	report := newReport(
		testIdentity(),
		fixedLiveness(testIdentity().Version, start, 90*time.Minute),
		counts,
		counts,
	)

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
}

func TestSelfSeedKeepsIdentityFields(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	counts := stubCounter{}
	report := newReport(
		testIdentity(),
		fixedLiveness(testIdentity().Version, start, 0),
		counts,
		counts,
	)

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
}

func TestSelfSeedCountErrorsReportZero(t *testing.T) {
	start := time.Date(2026, time.June, 22, 10, 0, 0, 0, time.UTC)
	counts := stubCounter{rwi: 5, urls: 9, err: errors.New("boom")}
	report := newReport(
		testIdentity(),
		fixedLiveness(testIdentity().Version, start, time.Hour),
		counts,
		counts,
	)

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
	report := newReport(
		testIdentity(),
		fixedLiveness(testIdentity().Version, start, 45*time.Minute),
		stubCounter{},
		stubCounter{},
	)

	ctx := context.Background()

	if got := report.Version(ctx); got != "1.2" {
		t.Fatalf("Version = %q, want 1.2", got)
	}
	if got := report.Uptime(ctx); got != 45 {
		t.Fatalf("Uptime = %d, want 45", got)
	}
}
