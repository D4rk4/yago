package peerroster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func answeredOrigin(t *testing.T, origin string) peerwire.Endpoint {
	t.Helper()
	endpoint, err := peerwire.ParseEndpointOrigin(origin)
	if err != nil {
		t.Fatal(err)
	}

	return endpoint
}

func assertAnsweredEndpoint(
	t *testing.T,
	r *roster,
	peer yagomodel.Hash,
	want peerwire.Endpoint,
) {
	t.Helper()
	got, found, err := r.AnsweredEndpoint(t.Context(), peer)
	if err != nil || !found || got != want {
		t.Fatalf("answered endpoint = %+v/%t/%v, want %+v", got, found, err, want)
	}
}

func assertNoAnsweredEndpoint(t *testing.T, r *roster, peer yagomodel.Hash) {
	t.Helper()
	got, found, err := r.AnsweredEndpoint(t.Context(), peer)
	if err != nil || found {
		t.Fatalf("answered endpoint = %+v/%t/%v, want none", got, found, err)
	}
}

func assertNoAnsweredEndpointRow(
	t *testing.T,
	r *roster,
	engine *scriptedEngine,
	peer yagomodel.Hash,
) {
	t.Helper()
	if _, stored := engine.buckets[answeredEndpointsBucket][string(r.key(peer))]; stored {
		t.Fatalf("answered endpoint row stored for %s", peer)
	}
}

func observeAnsweredAt(t *testing.T, r *roster, peer yagomodel.Seed, origin string) {
	t.Helper()
	r.ObserveAnsweredResponder(t.Context(), peer, answeredOrigin(t, origin))
}

func TestObserveAnsweredResponderStoresExactVerifiedContact(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	answered := answeredOrigin(t, "http://203.0.113.1:8090")

	r.ObserveAnsweredResponder(t.Context(), peer, answered)

	assertAnsweredEndpoint(t, r, peer.Hash, answered)
	r.ObserveResponder(t.Context(), peer)
	assertAnsweredEndpoint(t, r, peer.Hash, answered)
	reopened := reopenScriptedRoster(t, engine, r.now)
	assertAnsweredEndpoint(t, reopened, peer.Hash, answered)
}

func TestAnsweredObservationRefusesUnverifiedOrUnadvertisedContact(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	junior := internalSeed(t, "junior", "203.0.113.2")
	r.ObserveAnsweredCaller(
		t.Context(), junior, yagomodel.PeerJunior, answeredOrigin(t, "http://203.0.113.2:8090"),
	)
	if r.KnownPeerCount(t.Context()) != 1 {
		t.Fatal("junior caller observation was not stored")
	}
	assertNoAnsweredEndpointRow(t, r, engine, junior.Hash)

	verified := internalSeed(t, "verified", "203.0.113.4")
	for _, origin := range []string{
		"http://192.0.2.9:8090",
		"http://203.0.113.4:8091",
		"https://203.0.113.4:443",
	} {
		observeAnsweredAt(t, r, verified, origin)
	}
	r.ObserveAnsweredResponder(t.Context(), verified, peerwire.Endpoint{
		Scheme: "http", Host: "user@203.0.113.4", Port: "8090",
	})
	assertNoAnsweredEndpoint(t, r, verified.Hash)
	assertNoAnsweredEndpointRow(t, r, engine, verified.Hash)

	impostor := internalSeed(t, "impostor", "203.0.113.4")
	observeAnsweredAt(t, r, impostor, "http://203.0.113.4:8090")
	assertNoAnsweredEndpointRow(t, r, engine, impostor.Hash)
}

func TestAnsweredEndpointIgnoresChangedAdvertisement(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")

	moved := peer.Copy()
	moved.Port = yagomodel.Some(yagomodel.Port(8091))
	r.ObserveResponder(t.Context(), moved)

	assertNoAnsweredEndpoint(t, r, peer.Hash)
}

func TestAnsweredEndpointSurvivesReorderedSameAdvertisement(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	alternate, err := yagomodel.ParseHost("198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	peer.IP6 = yagomodel.Some([]yagomodel.Host{alternate})
	answered := answeredOrigin(t, "http://198.51.100.7:8090")
	r.ObserveAnsweredResponder(t.Context(), peer, answered)

	r.ObserveResponder(t.Context(), peer.WithPrimaryHost(alternate))

	assertAnsweredEndpoint(t, r, peer.Hash, answered)
}

func TestAnsweredEndpointIgnoresRecordBoundToAnotherPeer(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.1")
	second.Port = yagomodel.Some(yagomodel.Port(8091))
	observeAnsweredAt(t, r, first, "http://203.0.113.1:8090")
	r.ObserveResponder(t.Context(), second)
	answeredRows := engine.buckets[answeredEndpointsBucket]
	answeredRows[string(r.key(second.Hash))] = answeredRows[string(r.key(first.Hash))]

	assertNoAnsweredEndpoint(t, r, second.Hash)
}

func TestAnsweredEndpointDegradesWhenSidecarsAreMissingOrMalformed(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")
	key := string(r.key(peer.Hash))

	lifecycle := engine.buckets[peerLifecyclesBucket][key]
	delete(engine.buckets[peerLifecyclesBucket], key)
	assertNoAnsweredEndpoint(t, r, peer.Hash)
	engine.buckets[peerLifecyclesBucket][key] = lifecycle
	assertAnsweredEndpoint(t, r, peer.Hash, answeredOrigin(t, "http://203.0.113.1:8090"))

	delete(engine.buckets[answeredEndpointsBucket], key)
	assertNoAnsweredEndpoint(t, r, peer.Hash)

	orphan := internalHashFor("orphan")
	engine.buckets[answeredEndpointsBucket][string(r.key(orphan))] = []byte(
		"YPE1" + string(make([]byte, 32)) + "http://203.0.113.9:8090",
	)
	assertNoAnsweredEndpoint(t, r, orphan)
}

func TestAnsweredEndpointReportsUnreadablePeerRow(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.ObserveResponder(t.Context(), peer)
	corruptPeerRecord(t, r, engine, peer.Hash)

	if _, found, err := r.AnsweredEndpoint(t.Context(), peer.Hash); err == nil || found {
		t.Fatalf("unreadable peer row = %t/%v", found, err)
	}
}

func TestAnsweredEndpointWritesNothingWhenStorageRefuses(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	answered := answeredOrigin(t, "http://203.0.113.1:8090")

	engine.putErrors[answeredEndpointsBucket] = errors.New("put failed")
	r.ObserveAnsweredResponder(t.Context(), peer, answered)
	delete(engine.putErrors, answeredEndpointsBucket)
	assertNoAnsweredEndpointRow(t, r, engine, peer.Hash)
	if r.ReachablePeerCount(t.Context()) != 0 {
		t.Fatal("refused answered endpoint write still admitted the observation")
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	r.ObserveAnsweredResponder(canceled, peer, answered)
	assertNoAnsweredEndpointRow(t, r, engine, peer.Hash)
	if _, found, err := r.AnsweredEndpoint(canceled, peer.Hash); err == nil || found {
		t.Fatalf("canceled answered endpoint lookup = %t/%v", found, err)
	}
}

func TestDeletingRosterEntryRemovesAnsweredEndpoint(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")
	deleteEntry := func() error {
		return r.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, err := r.deleteRosterEntry(tx, r.key(peer.Hash))

			return err
		})
	}

	engine.deleteErrors[answeredEndpointsBucket] = errors.New("delete failed")
	if err := deleteEntry(); err == nil {
		t.Fatal("answered endpoint deletion failure was hidden")
	}
	delete(engine.deleteErrors, answeredEndpointsBucket)
	if err := deleteEntry(); err != nil {
		t.Fatal(err)
	}

	assertNoAnsweredEndpointRow(t, r, engine, peer.Hash)
	r.ObserveResponder(t.Context(), peer)
	assertNoAnsweredEndpoint(t, r, peer.Hash)
}

func TestOpenReportsAnsweredEndpointRegistrationFailure(t *testing.T) {
	engine := newScriptedEngine()
	engine.provisionErrors[answeredEndpointsBucket] = errors.New("answered provision failed")
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(
		t.Context(),
		storage,
		internalHashFor("local"),
		time.Now,
		Capacity{Reservoir: 8, Active: 4},
	); err == nil {
		t.Fatal("answered endpoint registration failure was hidden")
	}
}

func TestAnsweredEndpointCodecRoundTripsAndRefusesForeignRows(t *testing.T) {
	codec := answeredEndpointCodec{}
	record := answeredEndpointRecord{endpoint: answeredOrigin(t, "https://[2001:db8::7]:8443")}
	record.advertisementIdentity[0] = 7
	raw, err := codec.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(raw)
	if err != nil || decoded != record {
		t.Fatalf("decoded = %+v/%v, want %+v", decoded, err, record)
	}

	header := string(raw[:answeredEndpointHeaderWidth])
	for _, refused := range []string{
		header[:answeredEndpointHeaderWidth-1],
		"YPE0" + header[len(answeredEndpointMagic):] + "http://192.0.2.7:8090",
		header,
		header + "http://user@192.0.2.7:8090",
		header + "http://192.0.2.7:8090/yacy",
		header + "http://192.0.2.7:8090?peer=1",
		header + "http://192.0.2.7:8090#peer",
		header + "ftp://192.0.2.7:21",
	} {
		if _, err := codec.Decode([]byte(refused)); err == nil {
			t.Fatalf("Decode(%q) accepted a foreign row", refused)
		}
	}
}

func TestUnaddressableVerifiedEntryCarriesNoAdvertisement(t *testing.T) {
	entry := rosterEntry{seed: yagomodel.Seed{Hash: internalHashFor("bare")}, verified: true}
	if _, _, ok := verifiedAdvertisement(entry); ok {
		t.Fatal("unaddressable entry produced an advertisement identity")
	}
	entry.seed = internalSeed(t, "bare", "203.0.113.1")
	if _, _, ok := verifiedAdvertisement(entry); !ok {
		t.Fatal("addressable verified entry produced no advertisement identity")
	}
}
