package yagonode

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
)

func TestRecordHostLinkBoundsRetainedGraph(t *testing.T) {
	incoming := map[string]map[string]hostLinkReference{}
	capacity := hostLinkCapacity{linkedHosts: 2, referencesPerHost: 2}
	recordHostLink(incoming, "target-a", "source-a", 1, capacity)
	recordHostLink(incoming, "target-a", "source-a", 2, capacity)
	recordHostLink(incoming, "target-a", "source-b", 1, capacity)
	recordHostLink(incoming, "target-a", "source-c", 3, capacity)
	recordHostLink(incoming, "target-b", "source-a", 1, capacity)
	recordHostLink(incoming, "target-c", "source-a", 1, capacity)

	if len(incoming) != 2 || len(incoming["target-a"]) != 2 {
		t.Fatalf("retained graph = %#v", incoming)
	}
	if got := incoming["target-a"]["source-a"]; got.Count != 2 || got.ModifiedDay != 2 {
		t.Fatalf("retained reference = %#v", got)
	}
	if _, found := incoming["target-a"]["source-c"]; found {
		t.Fatal("source beyond the per-target cap was retained")
	}
	if _, found := incoming["target-c"]; found {
		t.Fatal("target beyond the graph cap was retained")
	}
}

func TestCachedStoredDocumentHostLinksCachesSuccessfulScan(t *testing.T) {
	now := time.Unix(100, 0)
	var scans atomic.Int32
	source := &cachedStoredDocumentHostLinks{
		now: func() time.Time { return now },
		scan: func(context.Context) (hostlinks.Graph, error) {
			scans.Add(1)

			return hostlinks.Graph{
				RowDefinition: hostlinks.HostReferenceRowDefinition,
				LinkedHosts:   []hostlinks.LinkedHost{{HostHash: "target"}},
			}, nil
		},
	}

	first := source.IncomingHostLinks(t.Context())
	second := source.IncomingHostLinks(t.Context())
	if scans.Load() != 1 || len(first.LinkedHosts) != 1 || len(second.LinkedHosts) != 1 {
		t.Fatalf("scans=%d first=%#v second=%#v", scans.Load(), first, second)
	}
	now = now.Add(hostLinkSnapshotTTL)
	source.IncomingHostLinks(t.Context())
	if scans.Load() != 2 {
		t.Fatalf("scans after expiry = %d, want 2", scans.Load())
	}
}

func TestCachedStoredDocumentHostLinksRetriesFailedScan(t *testing.T) {
	sentinel := errors.New("scan failed")
	var scans atomic.Int32
	source := &cachedStoredDocumentHostLinks{
		now: time.Now,
		scan: func(context.Context) (hostlinks.Graph, error) {
			if scans.Add(1) == 1 {
				return hostlinks.Graph{}, sentinel
			}

			return hostlinks.Graph{RowDefinition: hostlinks.HostReferenceRowDefinition}, nil
		},
	}

	failed := source.IncomingHostLinks(t.Context())
	succeeded := source.IncomingHostLinks(t.Context())
	if scans.Load() != 2 || failed.RowDefinition != hostlinks.HostReferenceRowDefinition ||
		succeeded.RowDefinition != hostlinks.HostReferenceRowDefinition {
		t.Fatalf("scans=%d failed=%#v succeeded=%#v", scans.Load(), failed, succeeded)
	}
}

func TestCachedStoredDocumentHostLinksSerializesRefresh(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var scans atomic.Int32
	source := &cachedStoredDocumentHostLinks{
		now: time.Now,
		scan: func(context.Context) (hostlinks.Graph, error) {
			scans.Add(1)
			close(started)
			<-release

			return hostlinks.Graph{RowDefinition: hostlinks.HostReferenceRowDefinition}, nil
		},
	}
	done := make(chan struct{}, 2)
	go func() {
		source.IncomingHostLinks(t.Context())
		done <- struct{}{}
	}()
	<-started
	go func() {
		source.IncomingHostLinks(t.Context())
		done <- struct{}{}
	}()
	close(release)
	<-done
	<-done

	if scans.Load() != 1 {
		t.Fatalf("concurrent scans = %d, want 1", scans.Load())
	}
}

func TestNewCachedStoredDocumentHostLinksUsesStoredScanner(t *testing.T) {
	source := newCachedStoredDocumentHostLinks(storedDocumentHostLinks{})
	graph := source.IncomingHostLinks(t.Context())
	if graph.RowDefinition != hostlinks.HostReferenceRowDefinition {
		t.Fatalf("graph = %#v", graph)
	}
}
