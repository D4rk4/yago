package peerroster

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestEndpointOwnershipRebuildClearsAZeroCapacityRoster(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	r.reservoirCap = 0
	r.endpointOwners["203.0.113.1:8090"] = endpointOwnership{peer: internalHashFor("peer")}

	if err := r.rebuildEndpointOwnership(t.Context()); err != nil {
		t.Fatalf("rebuild endpoint ownership: %v", err)
	}
	if len(r.endpointOwnershipSnapshot()) != 0 {
		t.Fatal("zero-capacity endpoint ownership was not cleared")
	}
}

func TestEndpointOwnershipRebuildFiltersSelfAndExpiredRows(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	self := internalSeed(t, "local", "203.0.113.1")
	expired := internalSeed(t, "expired", "203.0.113.2")
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := r.putRosterEntry(tx, r.key(self.Hash), rosterEntry{
			seed: self, lastSeen: now, expiresAt: now.Add(time.Hour), verified: true,
		}); err != nil {
			return fmt.Errorf("store self endpoint row: %w", err)
		}

		if err := r.putRosterEntry(tx, r.key(expired.Hash), rosterEntry{
			seed: expired, lastSeen: now, expiresAt: now.Add(-time.Second), verified: true,
		}); err != nil {
			return fmt.Errorf("store expired endpoint row: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("store endpoint rows: %v", err)
	}

	if err := r.rebuildEndpointOwnership(t.Context()); err != nil {
		t.Fatalf("rebuild endpoint ownership: %v", err)
	}
	if len(r.endpointOwnershipSnapshot()) != 0 {
		t.Fatal("self or expired row retained endpoint ownership")
	}
}

func TestEndpointOwnershipRebuildSurfacesCancellationAndStorageFailure(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		r, engine := openScriptedRoster(t, 8, 4)
		r.Discover(t.Context(), internalSeed(t, "peer", "203.0.113.1"))
		ctx, cancel := context.WithCancel(t.Context())
		engine.scanObserver = func(vault.Name) { cancel() }

		if err := r.rebuildEndpointOwnership(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled endpoint rebuild error = %v", err)
		}
	})

	t.Run("storage failure", func(t *testing.T) {
		r, engine := openScriptedRoster(t, 8, 4)
		engine.scanErrors[peersBucket] = errors.New("scan failed")

		if err := r.rebuildEndpointOwnership(t.Context()); err == nil {
			t.Fatal("endpoint rebuild accepted a storage failure")
		}
	})
}

func TestEndpointAdmissionSortsEveryDisplacedOwner(t *testing.T) {
	claim := internalSeed(t, "claim", "203.0.113.1")
	claim.IP6 = yagomodel.Some([]yagomodel.Host{parseCandidateHost(t, "2001:db8::1")})
	entry := verifiedRosterEntry(claim, time.Unix(100, 0))
	endpoints := advertisedPeerEndpoints(claim)
	first := internalHashFor("AAAAAAAAAAAA")
	second := internalHashFor("BBBBBBBBBBBB")
	owners := map[string]endpointOwnership{
		endpoints[0]: {peer: second},
		endpoints[1]: {peer: first},
	}

	admission := endpointAdmissionAgainst(owners, entry)

	if !admission.accepted || len(admission.displaced) != 2 ||
		admission.displaced[0] != first || admission.displaced[1] != second {
		t.Fatalf("endpoint admission = %#v", admission)
	}
}

func TestAddresslessEntryCannotOwnAnEndpoint(t *testing.T) {
	entry := rosterEntry{seed: yagomodel.Seed{Hash: internalHashFor("peer")}}
	if entryOwnsAdvertisedEndpoints(map[string]endpointOwnership{}, entry) {
		t.Fatal("addressless entry owns an endpoint")
	}
}
