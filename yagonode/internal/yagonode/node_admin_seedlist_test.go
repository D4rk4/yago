package yagonode

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/seedimport"
)

type fakeSeedImporter struct {
	seeds  []yagomodel.Seed
	err    error
	called []string
}

func (f *fakeSeedImporter) Import(_ context.Context, url string) ([]yagomodel.Seed, error) {
	f.called = append(f.called, url)

	return f.seeds, f.err
}

type fakeRosterSink struct {
	discovered []yagomodel.Seed
}

func (f *fakeRosterSink) Discover(_ context.Context, seeds ...yagomodel.Seed) {
	f.discovered = append(f.discovered, seeds...)
}

type recordedImport struct {
	url   string
	seeds int
	err   error
}

type fakeSeedRecorder struct {
	calls []recordedImport
	err   error
}

func (f *fakeSeedRecorder) Record(_ context.Context, url string, seeds int, importErr error) error {
	f.calls = append(f.calls, recordedImport{url: url, seeds: seeds, err: importErr})

	return f.err
}

type recordedEvent struct {
	severity events.Severity
	category events.Category
	name     string
}

type fakeEventRecorder struct {
	events []recordedEvent
}

func (f *fakeEventRecorder) Record(
	severity events.Severity,
	category events.Category,
	name, _ string,
) {
	f.events = append(f.events, recordedEvent{severity: severity, category: category, name: name})
}

func TestRefreshSeedlistRejectsUnknownURL(t *testing.T) {
	importer := &fakeSeedImporter{}
	source := newSeedlistRefreshSource(
		importer, &fakeRosterSink{}, &fakeSeedRecorder{}, &fakeEventRecorder{},
		[]string{"https://known/"},
	)

	if err := source.RefreshSeedlist(
		context.Background(),
		"https://evil/",
	); !errors.Is(
		err,
		errUnknownSeedlist,
	) {
		t.Fatalf("err = %v, want errUnknownSeedlist", err)
	}
	if len(importer.called) != 0 {
		t.Fatal("an unknown URL must never be fetched")
	}
}

func TestRefreshSeedlistImportsAndDiscovers(t *testing.T) {
	seeds := []yagomodel.Seed{networkTestSeed(t), networkTestSeed(t)}
	importer := &fakeSeedImporter{seeds: seeds}
	roster := &fakeRosterSink{}
	store := &fakeSeedRecorder{}
	recorder := &fakeEventRecorder{}
	source := newSeedlistRefreshSource(
		importer,
		roster,
		store,
		recorder,
		[]string{"https://known/"},
	)

	if err := source.RefreshSeedlist(context.Background(), "https://known/"); err != nil {
		t.Fatalf("RefreshSeedlist: %v", err)
	}
	if len(roster.discovered) != 2 {
		t.Fatalf("discovered %d seeds, want 2", len(roster.discovered))
	}
	if len(store.calls) != 1 || store.calls[0].seeds != 2 || store.calls[0].err != nil {
		t.Fatalf("record calls = %+v", store.calls)
	}
	if len(recorder.events) != 1 || recorder.events[0].name != "seedlist.refreshed" ||
		recorder.events[0].severity != events.SeverityInfo {
		t.Fatalf("events = %+v", recorder.events)
	}
}

func TestRefreshSeedlistRecordsImportFailure(t *testing.T) {
	importer := &fakeSeedImporter{err: errors.New("fetch failed")}
	roster := &fakeRosterSink{}
	store := &fakeSeedRecorder{}
	recorder := &fakeEventRecorder{}
	source := newSeedlistRefreshSource(
		importer,
		roster,
		store,
		recorder,
		[]string{"https://known/"},
	)

	err := source.RefreshSeedlist(context.Background(), "https://known/")
	if err == nil {
		t.Fatal("a failed import should be returned")
	}
	if len(roster.discovered) != 0 {
		t.Fatal("nothing should be discovered on a failed import")
	}
	if len(store.calls) != 1 || store.calls[0].err == nil {
		t.Fatalf("failure should be recorded: %+v", store.calls)
	}
	if len(recorder.events) != 1 || recorder.events[0].name != "seedlist.refresh.failed" ||
		recorder.events[0].severity != events.SeverityWarn {
		t.Fatalf("events = %+v", recorder.events)
	}
}

func TestRefreshSeedlistToleratesRecordError(t *testing.T) {
	importer := &fakeSeedImporter{seeds: []yagomodel.Seed{networkTestSeed(t)}}
	store := &fakeSeedRecorder{err: errors.New("disk full")}
	source := newSeedlistRefreshSource(
		importer, &fakeRosterSink{}, store, &fakeEventRecorder{}, []string{"https://known/"},
	)

	if err := source.RefreshSeedlist(context.Background(), "https://known/"); err != nil {
		t.Fatalf("a status-store write failure must not fail the refresh: %v", err)
	}
}

type fakeSeedStatus struct {
	statuses map[string]seedimport.Status
	err      error
}

func (f fakeSeedStatus) Get(_ context.Context, url string) (seedimport.Status, bool, error) {
	if f.err != nil {
		return seedimport.Status{}, false, f.err
	}
	status, ok := f.statuses[url]

	return status, ok, nil
}

func TestNetworkSourceSurfacesSeedlistStatus(t *testing.T) {
	url := "https://seeds.example/seed.txt"
	imported := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	status := fakeSeedStatus{statuses: map[string]seedimport.Status{
		url: {LastImport: imported, Seeds: 12, OK: true},
	}}
	source := newNetworkSource(dhtGateStatusSource{}, nil, []string{url}, status, nil)

	entries := source.Network(context.Background()).Seedlists
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	entry := entries[0]
	if !entry.Imported || !entry.OK || entry.Result != "12 seeds" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.LastImport != "2026-07-04T09:00:00Z" {
		t.Fatalf("last import = %q", entry.LastImport)
	}
}

func TestNetworkSourceIgnoresSeedlistStatusError(t *testing.T) {
	url := "https://seeds.example/seed.txt"
	source := newNetworkSource(
		dhtGateStatusSource{},
		nil,
		[]string{url},
		fakeSeedStatus{err: errors.New("read failed")},
		nil,
	)

	entries := source.Network(context.Background()).Seedlists
	if len(entries) != 1 || entries[0].Imported {
		t.Fatalf("a status read error should leave the entry un-imported: %+v", entries)
	}
}

func TestSeedImportSourcesBuildsStatusAndRefresh(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	assembled := node{vault: v, roster: reachableRoster{}, client: &http.Client{}}
	config := nodeConfig{SeedlistURLs: []string{"https://seeds.example/seed.txt"}}

	status, refresh := seedImportSources(assembled, config, events.NewRecorder(8))
	if status == nil {
		t.Fatal("expected a durable status reader")
	}
	if refresh == nil {
		t.Fatal("expected a refresh action when a roster, client, and URLs are present")
	}
}

func TestSeedImportSourcesWithoutRosterHasNoRefresh(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	config := nodeConfig{SeedlistURLs: []string{"https://seeds.example/seed.txt"}}

	status, refresh := seedImportSources(node{vault: v}, config, events.NewRecorder(8))
	if status == nil {
		t.Fatal("the status reader should still be available for history")
	}
	if refresh != nil {
		t.Fatal("no refresh action without a roster and client")
	}
}

func TestSeedImportSourcesWithoutVault(t *testing.T) {
	status, refresh := seedImportSources(node{}, nodeConfig{}, events.NewRecorder(8))
	if status != nil || refresh != nil {
		t.Fatal("no seed-import sources without a vault")
	}
}

func TestSeedImportSourcesDegradesOnOpenError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	status, refresh := seedImportSources(node{vault: v}, nodeConfig{}, events.NewRecorder(8))
	if status != nil || refresh != nil {
		t.Fatal("a store-open failure should degrade to no sources")
	}
}

func TestSeedlistResultFormats(t *testing.T) {
	cases := []struct {
		name   string
		status seedimport.Status
		want   string
	}{
		{"ok", seedimport.Status{OK: true, Seeds: 3}, "3 seeds"},
		{"failed-with-message", seedimport.Status{Error: "boom"}, "failed: boom"},
		{"failed-bare", seedimport.Status{}, "failed"},
	}
	for _, tc := range cases {
		if got := seedlistResult(tc.status); got != tc.want {
			t.Fatalf("%s: seedlistResult = %q, want %q", tc.name, got, tc.want)
		}
	}
}
