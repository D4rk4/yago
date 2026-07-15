package documentstore

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestReplaceOutboundAnchorsAllowsDisjointSourcesAndTargets(t *testing.T) {
	_, receiver, engine := openPagedDocuments(t)
	documents := receiver.(documentVault)
	sources := distinctStoredDocumentBoundaryURLs(
		documents.outboundBoundaries,
		2,
		"https://sources.example/",
	)
	targets := distinctStoredDocumentBoundaryURLs(
		documents.urlBoundaries,
		2,
		"https://targets.example/",
	)
	releaseUpdates := make(chan struct{})
	bothEntered := make(chan struct{})
	var entered atomic.Int64
	engine.beforeUpdate = func() {
		if entered.Add(1) == 2 {
			close(bothEntered)
		}
		<-releaseUpdates
	}
	results := make(chan error, 2)
	for index := range 2 {
		go func() {
			anchors := anchorReceiver(t, receiver)
			update, err := anchors.ReplaceOutboundAnchors(
				t.Context(),
				[]OutboundAnchorSet{{
					SourceURL: sources[index],
					Anchors:   []OutboundAnchor{{TargetURL: targets[index]}},
				}},
			)
			anchors.ReleaseOutboundAnchors(update.Finalizations)
			results <- err
		}()
	}
	select {
	case <-bothEntered:
	case <-time.After(time.Second):
		close(releaseUpdates)
		t.Fatalf("disjoint anchor updates entered = %d, want 2", entered.Load())
	}
	close(releaseUpdates)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestReplaceOutboundAnchorsSerializesSameSource(t *testing.T) {
	_, receiver, engine := openPagedDocuments(t)
	source := "https://source.example/shared"
	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var entered atomic.Int64
	engine.beforeUpdate = func() {
		switch entered.Add(1) {
		case 1:
			close(firstEntered)
			<-releaseFirst
		case 2:
			close(secondEntered)
		}
	}
	results := make(chan error, 2)
	start := func(target string) {
		go func() {
			anchors := anchorReceiver(t, receiver)
			update, err := anchors.ReplaceOutboundAnchors(
				t.Context(),
				[]OutboundAnchorSet{{
					SourceURL: source,
					Anchors:   []OutboundAnchor{{TargetURL: target}},
				}},
			)
			anchors.ReleaseOutboundAnchors(update.Finalizations)
			results <- err
		}()
	}
	start("https://target.example/first")
	<-firstEntered
	start("https://target.example/second")
	select {
	case <-secondEntered:
		t.Fatal("same-source anchor update crossed source lock")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("same-source anchor update remained blocked")
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
