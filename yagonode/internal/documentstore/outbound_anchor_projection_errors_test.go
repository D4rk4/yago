package documentstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOutboundAnchorProjectionValidatesActiveLease(t *testing.T) {
	_, receiver := openDocuments(t)
	anchors := anchorReceiver(t, receiver)
	if err := anchors.VisitOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorFinalization{{sourceURL: "https://source.example/nil"}},
		func([]Document) error { return nil },
	); err == nil {
		t.Fatal("nil projection lease was accepted")
	}
	released := newOutboundAnchorLease(func() {}, nil)
	released.close()
	if err := anchors.VisitOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorFinalization{{
			sourceURL: "https://source.example/released",
			lease:     released,
		}},
		func([]Document) error { return nil },
	); err == nil {
		t.Fatal("released projection lease was accepted")
	}
	first := newOutboundAnchorLease(func() {}, nil)
	second := newOutboundAnchorLease(func() {}, nil)
	if err := anchors.VisitOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorFinalization{
			{sourceURL: "https://source.example/first", lease: first},
			{sourceURL: "https://source.example/second", lease: second},
		},
		func([]Document) error { return nil },
	); err == nil {
		t.Fatal("mixed projection leases were accepted")
	}
	anchors.ReleaseOutboundAnchors([]OutboundAnchorFinalization{
		{lease: first},
		{lease: second},
		{},
	})
	shared := newOutboundAnchorLease(func() {}, nil)
	tooMany := make([]OutboundAnchorFinalization, MaximumOutboundAnchorSourcesPerReplacement+1)
	for index := range tooMany {
		tooMany[index] = OutboundAnchorFinalization{
			sourceURL: fmt.Sprintf("https://source.example/%02d", index),
			lease:     shared,
		}
	}
	if err := anchors.VisitOutboundAnchorDocuments(
		t.Context(),
		tooMany,
		func([]Document) error { return nil },
	); err == nil {
		t.Fatal("oversized projection token set was accepted")
	}
	anchors.ReleaseOutboundAnchors(tooMany)
}

func TestOutboundAnchorProjectionSurfacesReadVisitAndContextFailures(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	missingLease := newOutboundAnchorLease(
		func() {},
		[]string{"https://missing.example/"},
	)
	visited := false
	if err := documents.VisitOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorFinalization{{
			sourceURL: "https://source.example/missing",
			lease:     missingLease,
		}},
		func([]Document) error {
			visited = true

			return nil
		},
	); err != nil || visited {
		t.Fatalf("missing projection = %t/%v", visited, err)
	}
	missingLease.close()

	target := "https://target.example/page"
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	update, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{{
		SourceURL: "https://source.example/page",
		Anchors:   []OutboundAnchor{{TargetURL: target}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	visitErr := errors.New("visit failed")
	if err := documents.VisitOutboundAnchorDocuments(
		t.Context(),
		update.Finalizations,
		func([]Document) error { return visitErr },
	); !errors.Is(err, visitErr) {
		t.Fatalf("visit error = %v", err)
	}
	documents.ReleaseOutboundAnchors(update.Finalizations)

	location := engine.buckets[documentLocationBucketName][target]
	engine.buckets[documentLocationBucketName][target] = []byte("invalid")
	readLease := newOutboundAnchorLease(func() {}, []string{target})
	if err := documents.VisitOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorFinalization{{
			sourceURL: "https://source.example/read",
			lease:     readLease,
		}},
		func([]Document) error { return nil },
	); err == nil {
		t.Fatal("malformed projection location was accepted")
	}
	readLease.close()
	engine.buckets[documentLocationBucketName][target] = location

	ctx := &errAfterContext{
		Context:   t.Context(),
		remaining: 2,
		err:       context.Canceled,
	}
	if _, err := documents.readOutboundAnchorProjection(
		ctx,
		[]string{target},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("projection context error = %v", err)
	}
}

func TestReplaceOutboundAnchorsReleasesSourceAfterTargetWaitCancellation(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	target := "https://target.example/blocked"
	releaseTarget, err := documents.urlBoundaries.lockWrites(
		t.Context(),
		[]string{target},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := documents.ReplaceOutboundAnchors(ctx, []OutboundAnchorSet{{
			SourceURL: "https://source.example/page",
			Anchors:   []OutboundAnchor{{TargetURL: target}},
		}})
		result <- err
	}()
	select {
	case err := <-result:
		releaseTarget()
		t.Fatalf("target wait returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		releaseTarget()
		t.Fatalf("target wait cancellation = %v", err)
	}
	releaseTarget()
	update, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{{
		SourceURL: "https://source.example/page",
		Anchors:   []OutboundAnchor{{TargetURL: target}},
	}})
	if err != nil {
		t.Fatalf("replacement after cancellation: %v", err)
	}
	documents.ReleaseOutboundAnchors(update.Finalizations)
}

func TestReplaceOutboundAnchorsSurfacesCapacityAndPhaseReadFailures(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	engine.quotaBytes = 1
	capacityErr := errors.New("capacity failed")
	engine.usedBytesErr = capacityErr
	if _, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{{
		SourceURL: "https://source.example/capacity",
		Anchors:   []OutboundAnchor{{TargetURL: "https://target.example/capacity"}},
	}}); !errors.Is(err, capacityErr) {
		t.Fatalf("capacity error = %v", err)
	}
	engine.quotaBytes = 0
	engine.usedBytesErr = nil
	source := "https://source.example/read"
	engine.beforeUpdate = func() {
		engine.beforeUpdate = nil
		engine.buckets[outboundAnchorPublicationBucket][source] = []byte("invalid")
	}
	if _, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: "https://target.example/read"}},
	}}); err == nil {
		t.Fatal("phase publication read failure was accepted")
	}
}

func TestReplaceOutboundAnchorSetChecksContextAndTargetLocation(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/direct"
	target := "https://target.example/direct"
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	err := documents.vault.Update(t.Context(), func(tx *vault.Txn) error {
		returned, pending, err := documents.replaceOutboundAnchorSet(
			&errAfterContext{Context: t.Context(), err: context.Canceled},
			tx,
			OutboundAnchorSet{
				SourceURL: source,
				Anchors:   []OutboundAnchor{{TargetURL: target}},
			},
			map[string]struct{}{},
		)
		if returned.sourceURL != "" || pending {
			t.Fatalf("canceled finalization = %#v/%t", returned, pending)
		}

		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("target context error = %v", err)
	}
	location := engine.buckets[documentLocationBucketName][target]
	engine.buckets[documentLocationBucketName][target] = []byte("invalid")
	if _, err := documents.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: target}},
	}}); err == nil {
		t.Fatal("malformed target location was accepted")
	}
	engine.buckets[documentLocationBucketName][target] = location
}

func TestOutboundAnchorFinalizationHandlesCurrentAndUnreadablePublication(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/page"
	desired := flaggedOutboundAnchorPublication(source)
	firstLease := newOutboundAnchorLease(func() {}, desired.Targets)
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		[]OutboundAnchorFinalization{{
			sourceURL: source,
			desired:   desired,
			lease:     firstLease,
		}},
	); err != nil {
		t.Fatal(err)
	}
	idempotentLease := newOutboundAnchorLease(func() {}, desired.Targets)
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		[]OutboundAnchorFinalization{{
			sourceURL: source,
			desired:   desired,
			lease:     idempotentLease,
		}},
	); err != nil {
		t.Fatalf("idempotent finalization: %v", err)
	}
	readLease := newOutboundAnchorLease(func() {}, nil)
	engine.beforeUpdate = func() {
		engine.beforeUpdate = nil
		engine.buckets[outboundAnchorPublicationBucket][source] = []byte("invalid")
	}
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		[]OutboundAnchorFinalization{{
			sourceURL: source,
			desired:   desired,
			lease:     readLease,
		}},
	); err == nil {
		t.Fatal("finalization publication read failure was accepted")
	}
}

func TestOutboundAnchorFinalizationRejectsInvalidLeaseSets(t *testing.T) {
	_, receiver, _ := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/page"
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		[]OutboundAnchorFinalization{{sourceURL: source}},
	); err == nil {
		t.Fatal("missing finalization lease was accepted")
	}
	releasedLease := newOutboundAnchorLease(func() {}, nil)
	releasedLease.close()
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		[]OutboundAnchorFinalization{{
			sourceURL: source,
			lease:     releasedLease,
		}},
	); err == nil {
		t.Fatal("released finalization lease was accepted")
	}
	leftLease := newOutboundAnchorLease(func() {}, nil)
	rightLease := newOutboundAnchorLease(func() {}, nil)
	if err := documents.FinalizeOutboundAnchors(
		t.Context(),
		[]OutboundAnchorFinalization{
			{sourceURL: "https://source.example/left", lease: leftLease},
			{sourceURL: "https://source.example/right", lease: rightLease},
		},
	); err == nil {
		t.Fatal("mixed finalization leases were accepted")
	}
	tooManyLease := newOutboundAnchorLease(func() {}, nil)
	tooMany := make([]OutboundAnchorFinalization, MaximumOutboundAnchorSourcesPerReplacement+1)
	for index := range tooMany {
		tooMany[index] = OutboundAnchorFinalization{
			sourceURL: fmt.Sprintf("https://source.example/limit/%02d", index),
			lease:     tooManyLease,
		}
	}
	if err := documents.FinalizeOutboundAnchors(t.Context(), tooMany); err == nil {
		t.Fatal("oversized finalization set was accepted")
	}
	documents.ReleaseOutboundAnchors([]OutboundAnchorFinalization{
		{lease: leftLease},
		{lease: rightLease},
	})
}

func flaggedOutboundAnchorPublication(sourceURL string) outboundAnchorPublication {
	return desiredOutboundAnchorPublication(
		map[string][]AnchorText{
			"https://target.example/page": {{
				URL:           sourceURL,
				Text:          "flags",
				NoFollow:      true,
				UserGenerated: true,
				Sponsored:     true,
			}},
		},
		[]string{"https://target.example/page"},
	)
}

func TestCanonicalOutboundAnchorSetsBoundAcceptedInput(t *testing.T) {
	source := "https://source.example/page"
	anchors := make([]OutboundAnchor, maximumOutboundAnchors+2)
	anchors[0] = OutboundAnchor{TargetURL: source}
	for index := 1; index < len(anchors); index++ {
		anchors[index] = OutboundAnchor{
			TargetURL: fmt.Sprintf("https://target.example/%04d", index),
		}
	}
	sets, err := canonicalOutboundAnchorSets([]OutboundAnchorSet{{
		SourceURL: source,
		Anchors:   anchors,
	}})
	if err != nil || len(sets) != 1 || len(sets[0].Anchors) != maximumOutboundAnchors {
		t.Fatalf("bounded sets = %d/%v", len(sets[0].Anchors), err)
	}
	grouped, targets := canonicalOutboundAnchors(source, []OutboundAnchor{
		{},
		{TargetURL: source},
		{TargetURL: " https://target.example/valid "},
	})
	if len(grouped) != 1 || !slices.Equal(targets, []string{"https://target.example/valid"}) {
		t.Fatalf("canonical targets = %#v/%#v", grouped, targets)
	}
}
