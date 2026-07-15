package yagonode

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
)

func testHostHash(t *testing.T, rawURL string) string {
	t.Helper()
	hash, ok := documentHostHash(rawURL)
	if !ok {
		t.Fatalf("documentHostHash(%q) failed", rawURL)
	}

	return hash
}

func hostLinkGraphFromDocuments(documents ...documentstore.Document) hostlinks.Graph {
	accumulator := hostLinkAccumulator{incoming: map[string]map[string]hostLinkReference{}}
	for _, document := range documents {
		collectDocumentHostLinks(&accumulator, document)
	}

	return hostlinks.Graph{
		RowDefinition: hostlinks.HostReferenceRowDefinition,
		LinkedHosts:   hostLinkGraphHosts(accumulator.incoming),
	}
}

func TestHostLinkCollectionReturnsRowDefinitionWithoutDocuments(t *testing.T) {
	graph := hostLinkGraphFromDocuments()

	if graph.RowDefinition != hostlinks.HostReferenceRowDefinition {
		t.Fatalf("row definition = %q", graph.RowDefinition)
	}
	if len(graph.LinkedHosts) != 0 {
		t.Fatalf("linked hosts = %d, want 0", len(graph.LinkedHosts))
	}
}

func TestHostLinkCollectionCountsOutlinksPerSourceHost(t *testing.T) {
	fetched := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	document := documentstore.Document{
		NormalizedURL: "http://source.net/page.html",
		FetchedAt:     fetched,
		Outlinks: []string{
			"http://target.com/one.html",
			"http://target.com/two.html",
			"http://target.com/three.html",
			"http://source.net/internal.html",
			"",
		},
	}

	graph := hostLinkGraphFromDocuments(document)

	if len(graph.LinkedHosts) != 1 {
		t.Fatalf("linked hosts = %d, want 1", len(graph.LinkedHosts))
	}
	host := graph.LinkedHosts[0]
	if want := testHostHash(t, "http://target.com/"); host.HostHash != want {
		t.Fatalf("host hash = %q, want %q", host.HostHash, want)
	}
	if len(host.References) != 1 {
		t.Fatalf("references = %d, want 1", len(host.References))
	}
	day := fetched.Unix() / secondsPerDay
	want := fmt.Sprintf(
		`{"h":%q,"m":%q,"c":"3"}`,
		testHostHash(t, "http://source.net/"),
		strconv.FormatInt(day, 10),
	)
	if string(host.References[0]) != want {
		t.Fatalf("reference = %s, want %s", host.References[0], want)
	}
}

func TestHostLinkCollectionAccumulatesAcrossDocuments(t *testing.T) {
	documents := []documentstore.Document{
		{
			NormalizedURL: "http://source.net/a.html",
			IndexedAt:     time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC),
			Outlinks:      []string{"http://target.com/one.html"},
		},
		{
			NormalizedURL: "http://source.net/b.html",
			Outlinks:      []string{"http://target.com/two.html"},
		},
		{
			NormalizedURL: "http://other.org/c.html",
			Outlinks:      []string{"http://target.com/three.html"},
		},
	}

	graph := hostLinkGraphFromDocuments(documents...)

	if len(graph.LinkedHosts) != 1 {
		t.Fatalf("linked hosts = %d, want 1", len(graph.LinkedHosts))
	}
	if got := len(graph.LinkedHosts[0].References); got != 2 {
		t.Fatalf("references = %d, want 2", got)
	}
}

func TestDocumentModifiedDayUsesFetchedThenIndexedThenZero(t *testing.T) {
	fetched := time.Date(2026, 7, 2, 23, 59, 0, 0, time.UTC)
	fetchedDoc := documentstore.Document{FetchedAt: fetched}
	if got := documentModifiedDay(fetchedDoc); got != fetched.Unix()/secondsPerDay {
		t.Fatalf("fetched day = %d", got)
	}

	indexed := time.Date(2026, 6, 30, 1, 0, 0, 0, time.UTC)
	indexedDoc := documentstore.Document{IndexedAt: indexed}
	if got := documentModifiedDay(indexedDoc); got != indexed.Unix()/secondsPerDay {
		t.Fatalf("indexed day = %d", got)
	}

	if got := documentModifiedDay(documentstore.Document{}); got != 0 {
		t.Fatalf("zero-time day = %d, want 0", got)
	}
}

func TestFirstSortedKeysCapsResults(t *testing.T) {
	keys := firstSortedKeys(map[string]int{"c": 1, "a": 2, "b": 3}, 2)

	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("keys = %v, want [a b]", keys)
	}
}

func TestHostLinkCollectionSkipsDocumentsWithoutValidHost(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "   ",
		Outlinks:      []string{"http://target.com/one.html"},
	}

	graph := hostLinkGraphFromDocuments(document)

	if len(graph.LinkedHosts) != 0 {
		t.Fatalf("linked hosts = %d, want 0", len(graph.LinkedHosts))
	}
}
