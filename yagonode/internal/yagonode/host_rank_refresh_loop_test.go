package yagonode

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
)

type signalingStoredDocuments struct {
	docs  []documentstore.Document
	scans chan struct{}
}

type hostTrustPolicyFixture struct {
	policy  hosttrust.Policy
	changes chan struct{}
}

func (f hostTrustPolicyFixture) Current() hosttrust.Policy { return f.policy }

func (f hostTrustPolicyFixture) Changes() <-chan struct{} { return f.changes }

func (s signalingStoredDocuments) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	for _, doc := range s.docs {
		if again, err := visit(doc); err != nil || !again {
			return err
		}
	}
	s.scans <- struct{}{}

	return nil
}

func citedDocuments() []documentstore.Document {
	return []documentstore.Document{
		{
			NormalizedURL: "http://source.net/a.html",
			Outlinks:      []string{"http://target.com/one.html"},
		},
		{
			NormalizedURL: "http://other.org/b.html",
			Outlinks:      []string{"http://target.com/two.html"},
		},
	}
}

func TestHostRankRefreshOnceBuildsAuthorityTable(t *testing.T) {
	holder := hostrank.NewHolder()
	sweeper := hostRankSweeper{
		documents: scriptedStoredDocuments{docs: citedDocuments()},
		holder:    holder,
	}

	sweeper.refreshOnce(context.Background())

	table := holder.Current()
	target := hostrank.RegistrableDomain("http://target.com/one.html")
	source := hostrank.RegistrableDomain("http://source.net/a.html")
	if table.Rank(target) != 1 {
		t.Fatalf("cited host rank = %v, want normalized 1: %v", table.Rank(target), table)
	}
	if table.Rank(target) <= table.Rank(source) {
		t.Fatalf("cited host (%v) must outrank the citing host (%v)",
			table.Rank(target), table.Rank(source))
	}
}

func TestHostRankRefreshOnceAppliesTrustedTeleport(t *testing.T) {
	holder := hostrank.NewHolder()
	sweeper := hostRankSweeper{
		documents: scriptedStoredDocuments{docs: []documentstore.Document{
			{NormalizedURL: "https://a.example/", Outlinks: []string{"https://b.example/"}},
			{NormalizedURL: "https://b.example/", Outlinks: []string{"https://a.example/"}},
		}},
		holder: holder,
		trust: hostTrustPolicyFixture{policy: hosttrust.Policy{
			Blend: 0.5, Domains: []string{"a.example"},
		}},
	}

	sweeper.refreshOnce(context.Background())

	table := holder.Current()
	if table.Rank("a.example") <= table.Rank("b.example") {
		t.Fatalf("trusted authority = %#v", table)
	}
}

func TestDocumentAuthorityCitationsHonorAnchorRelations(t *testing.T) {
	doc := documentstore.Document{
		CanonicalURL:                "https://source.example/page",
		OutboundAnchorEvidenceKnown: true,
		Outlinks:                    []string{"https://legacy.example/"},
		OutboundAnchors: []documentstore.OutboundAnchor{
			{TargetURL: "https://trusted.example/", Text: "trusted"},
			{TargetURL: "https://nofollow.example/", NoFollow: true},
			{TargetURL: "https://community.example/", UserGenerated: true},
			{TargetURL: "https://promotion.example/", Sponsored: true},
		},
	}
	citations := documentAuthorityCitations(doc)
	if len(citations) != 1 || citations[0].SourceURL != doc.CanonicalURL ||
		citations[0].TargetURL != "https://trusted.example/" ||
		citations[0].Confidence != 1 {
		t.Fatalf("citations = %#v", citations)
	}
	doc.OutboundAnchorEvidenceKnown = false
	citations = documentAuthorityCitations(doc)
	if len(citations) != 1 || citations[0].TargetURL != "https://legacy.example/" ||
		citations[0].Confidence != 0.4 {
		t.Fatalf("legacy citations = %#v", citations)
	}
	if citations := documentAuthorityCitations(documentstore.Document{}); citations != nil {
		t.Fatalf("empty document citations = %#v", citations)
	}
}

func TestHostRankRefreshOnceIgnoresNilDocuments(t *testing.T) {
	holder := hostrank.NewHolder()

	hostRankSweeper{documents: nil, holder: holder}.refreshOnce(context.Background())

	if got := holder.Current(); len(got) != 0 {
		t.Fatalf("nil documents produced a table: %v", got)
	}
}

func TestHostRankRefreshOnceSkipsOnScanError(t *testing.T) {
	holder := hostrank.NewHolder()
	sweeper := hostRankSweeper{
		documents: scriptedStoredDocuments{err: errors.New("scan failed")},
		holder:    holder,
	}

	sweeper.refreshOnce(context.Background())

	if got := holder.Current(); len(got) != 0 {
		t.Fatalf("failed scan produced a table: %v", got)
	}
}

func TestHostRankRefreshOnceSkipsCanceledComputation(t *testing.T) {
	holder := hostrank.NewHolder()
	sweeper := hostRankSweeper{
		documents: scriptedStoredDocuments{docs: citedDocuments()},
		holder:    holder,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sweeper.refreshOnce(ctx)

	if got := holder.Current(); len(got) != 0 {
		t.Fatalf("canceled computation produced a table: %v", got)
	}
}

func TestHostRankRefreshLoopRunsAndStops(t *testing.T) {
	oldTicks := newHostRankRefreshTicks
	t.Cleanup(func() { newHostRankRefreshTicks = oldTicks })
	ticks := make(chan time.Time, 1)
	var stopped atomic.Bool
	newHostRankRefreshTicks = func(time.Duration) (<-chan time.Time, func()) {
		return ticks, func() { stopped.Store(true) }
	}

	holder := hostrank.NewHolder()
	sweeper := hostRankSweeper{
		documents: signalingStoredDocuments{docs: citedDocuments(), scans: make(chan struct{}, 4)},
		holder:    holder,
		trust:     hostTrustPolicyFixture{changes: make(chan struct{}, 1)},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runHostRankRefreshLoop(ctx, sweeper)
		close(done)
	}()

	scans := sweeper.documents.(signalingStoredDocuments).scans
	select {
	case <-scans:
	case <-time.After(time.Second):
		t.Fatal("immediate refresh did not run")
	}
	ticks <- time.Now()
	select {
	case <-scans:
	case <-time.After(time.Second):
		t.Fatal("ticked refresh did not run")
	}
	sweeper.trust.(hostTrustPolicyFixture).changes <- struct{}{}
	select {
	case <-scans:
	case <-time.After(time.Second):
		t.Fatal("trust change refresh did not run")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not stop after cancel")
	}
	if !stopped.Load() {
		t.Fatal("ticker was not stopped")
	}
}

func TestHostRankRefreshLoopStopsWithoutTrustSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runHostRankRefreshLoop(ctx, hostRankSweeper{})
}
