package peerannouncement

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestExternalReachabilityEvidenceAcceptsSeniorAndPrincipal(t *testing.T) {
	for _, classification := range []yagomodel.PeerType{
		yagomodel.PeerSenior,
		yagomodel.PeerPrincipal,
	} {
		t.Run(classification.String(), func(t *testing.T) {
			evidence := newExternalReachabilityEvidence(
				2,
				func() time.Time { return time.Unix(1, 0) },
			)
			evidence.Observe(yagomodel.Hash("AAAAAAAAAAAA"), classification)

			if !evidence.Reachable(t.Context()) {
				t.Fatalf("%s classification was not retained", classification)
			}
		})
	}
}

func TestExternalReachabilityEvidenceInvalidatesOnlyJuniorObserver(t *testing.T) {
	now := time.Unix(1, 0)
	evidence := newExternalReachabilityEvidence(
		4,
		func() time.Time { return now },
	)
	first := yagomodel.Hash("AAAAAAAAAAAA")
	second := yagomodel.Hash("BBBBBBBBBBBB")
	evidence.Observe(first, yagomodel.PeerSenior)
	evidence.Observe(second, yagomodel.PeerPrincipal)

	evidence.Observe(first, yagomodel.PeerJunior)

	if !evidence.Reachable(t.Context()) {
		t.Fatal("one junior report removed another observer's positive evidence")
	}
	if observation := evidence.observations[first]; observation.classification != yagomodel.PeerJunior {
		t.Fatalf("junior observer = %#v", observation)
	}
	if _, retained := evidence.observations[second]; !retained {
		t.Fatal("other observer evidence was removed")
	}

	evidence.Observe(second, yagomodel.PeerJunior)
	if evidence.Reachable(t.Context()) {
		t.Fatal("reachability remained after every positive observer was invalidated")
	}
}

func TestExternalReachabilityEvidenceReportsStrongestPeerType(t *testing.T) {
	evidence := newExternalReachabilityEvidence(
		4,
		func() time.Time { return time.Unix(1, 0) },
	)
	if classification, known := evidence.PeerType(
		t.Context(),
	); known ||
		classification != yagomodel.PeerJunior {
		t.Fatalf("empty classification = %q/%v", classification, known)
	}
	evidence.Observe(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.PeerJunior)
	if classification, known := evidence.PeerType(
		t.Context(),
	); !known ||
		classification != yagomodel.PeerJunior {
		t.Fatalf("junior classification = %q/%v", classification, known)
	}
	evidence.Observe(yagomodel.Hash("BBBBBBBBBBBB"), yagomodel.PeerSenior)
	if classification, known := evidence.PeerType(
		t.Context(),
	); !known ||
		classification != yagomodel.PeerSenior {
		t.Fatalf("senior classification = %q/%v", classification, known)
	}
	evidence.Observe(yagomodel.Hash("CCCCCCCCCCCC"), yagomodel.PeerPrincipal)
	if classification, known := evidence.PeerType(
		t.Context(),
	); !known ||
		classification != yagomodel.PeerSenior {
		t.Fatalf("principal classification = %q/%v", classification, known)
	}
}

func TestExternalReachabilitySnapshotCarriesSelectedObservationTime(t *testing.T) {
	now := time.Unix(1, 0)
	evidence := newExternalReachabilityEvidence(
		4,
		func() time.Time { return now },
	)
	evidence.Observe(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.PeerJunior)
	now = now.Add(time.Second)
	evidence.Observe(yagomodel.Hash("BBBBBBBBBBBB"), yagomodel.PeerSenior)
	positiveAt := now
	now = now.Add(time.Second)
	evidence.Observe(yagomodel.Hash("CCCCCCCCCCCC"), yagomodel.PeerJunior)

	snapshot := evidence.Snapshot(t.Context())
	if !snapshot.Known || snapshot.PeerType != yagomodel.PeerSenior ||
		!snapshot.ObservedAt.Equal(positiveAt) {
		t.Fatalf("positive snapshot = %+v", snapshot)
	}
	evidence.Observe(yagomodel.Hash("BBBBBBBBBBBB"), yagomodel.PeerVirgin)
	snapshot = evidence.Snapshot(t.Context())
	if !snapshot.Known || snapshot.PeerType != yagomodel.PeerJunior ||
		!snapshot.ObservedAt.Equal(now) {
		t.Fatalf("junior snapshot = %+v", snapshot)
	}
}

func TestExternalReachabilityEvidenceExpiresAtLifetime(t *testing.T) {
	now := time.Unix(1, 0)
	evidence := newExternalReachabilityEvidence(
		2,
		func() time.Time { return now },
	)
	evidence.Observe(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.PeerSenior)

	now = now.Add(externalReachabilityLifetime - time.Nanosecond)
	if !evidence.Reachable(t.Context()) {
		t.Fatal("evidence expired before its lifetime")
	}

	now = now.Add(time.Nanosecond)
	if evidence.Reachable(t.Context()) {
		t.Fatal("evidence remained reachable at its expiry boundary")
	}
	if snapshot := evidence.Snapshot(t.Context()); snapshot.Known {
		t.Fatalf("expired snapshot = %+v", snapshot)
	}
}

func TestExternalReachabilityEvidenceEvictsOldestObserverAtBound(t *testing.T) {
	now := time.Unix(1, 0)
	evidence := newExternalReachabilityEvidence(
		2,
		func() time.Time { return now },
	)
	first := yagomodel.Hash("AAAAAAAAAAAA")
	second := yagomodel.Hash("BBBBBBBBBBBB")
	third := yagomodel.Hash("CCCCCCCCCCCC")
	evidence.Observe(first, yagomodel.PeerSenior)
	now = now.Add(time.Second)
	evidence.Observe(second, yagomodel.PeerSenior)
	now = now.Add(time.Second)
	evidence.Observe(second, yagomodel.PeerPrincipal)
	evidence.Observe(third, yagomodel.PeerSenior)

	if len(evidence.observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(evidence.observations))
	}
	if _, retained := evidence.observations[first]; retained {
		t.Fatal("oldest observer was not evicted")
	}
	if _, retained := evidence.observations[second]; !retained {
		t.Fatal("refreshed observer was evicted")
	}
	if _, retained := evidence.observations[third]; !retained {
		t.Fatal("new observer was not retained")
	}
}

func TestExternalReachabilityEvidenceCoordinatesConcurrentObserversAndReaders(t *testing.T) {
	evidence := NewExternalReachabilityEvidence()
	var group sync.WaitGroup
	for sequence := range 64 {
		group.Add(2)
		go func() {
			defer group.Done()
			evidence.Observe(
				yagomodel.Hash(fmt.Sprintf("%012d", sequence)),
				[]yagomodel.PeerType{yagomodel.PeerSenior, yagomodel.PeerJunior}[sequence%2],
			)
		}()
		go func() {
			defer group.Done()
			_ = evidence.Reachable(t.Context())
			_, _ = evidence.PeerType(t.Context())
		}()
	}
	group.Wait()
	evidence.Observe(yagomodel.Hash("ZZZZZZZZZZZZ"), yagomodel.PeerSenior)
	if !evidence.Reachable(t.Context()) {
		t.Fatal("final positive observation was not readable")
	}
}
