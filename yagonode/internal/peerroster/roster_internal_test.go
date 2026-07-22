package peerroster

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const internalHashFiller = "AAAAAAAAAAAA"

type scriptedEngine struct {
	buckets         map[vault.Name]map[string][]byte
	provisionErrors map[vault.Name]error
	putErrors       map[vault.Name]error
	deleteErrors    map[vault.Name]error
	scanErrors      map[vault.Name]error
	keyPageError    error
	scanObserver    func(vault.Name)
	updates         atomic.Int32
	keyPages        atomic.Int32
	updateStarted   chan<- struct{}
	updateBlock     <-chan struct{}
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{
		buckets:         map[vault.Name]map[string][]byte{},
		provisionErrors: map[vault.Name]error{},
		putErrors:       map[vault.Name]error{},
		deleteErrors:    map[vault.Name]error{},
		scanErrors:      map[vault.Name]error{},
	}
}

func (e *scriptedEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if e.updateBlock != nil {
		if e.updateStarted != nil {
			select {
			case e.updateStarted <- struct{}{}:
			default:
			}
		}
		select {
		case <-e.updateBlock:
		case <-ctx.Done():
			return fmt.Errorf("context: %w", ctx.Err())
		}
	}
	e.updates.Add(1)
	return fn(scriptedTxn{engine: e, writable: true})
}

func (e *scriptedEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return fn(scriptedTxn{engine: e})
}

func (e *scriptedEngine) Provision(name vault.Name) error {
	if err := e.provisionErrors[name]; err != nil {
		return err
	}
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}
	return nil
}

func (e *scriptedEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *scriptedEngine) QuotaBytes() int64                        { return 0 }
func (e *scriptedEngine) Close() error                             { return nil }

type scriptedTxn struct {
	engine   *scriptedEngine
	writable bool
}

func (t scriptedTxn) Bucket(name vault.Name) vault.EngineBucket {
	return scriptedBucket{engine: t.engine, name: name}
}

func (t scriptedTxn) Writable() bool { return t.writable }

type scriptedBucket struct {
	engine *scriptedEngine
	name   vault.Name
}

func (b scriptedBucket) Get(key vault.Key) []byte {
	raw := b.engine.buckets[b.name][string(key)]
	if raw == nil {
		return nil
	}
	return append([]byte(nil), raw...)
}

func (b scriptedBucket) Put(key vault.Key, raw []byte) error {
	if err := b.engine.putErrors[b.name]; err != nil {
		return err
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), raw...)
	return nil
}

func (b scriptedBucket) Delete(key vault.Key) error {
	if err := b.engine.deleteErrors[b.name]; err != nil {
		return err
	}
	delete(b.engine.buckets[b.name], string(key))
	return nil
}

func (b scriptedBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	if err := b.engine.scanErrors[b.name]; err != nil {
		return err
	}
	if b.engine.scanObserver != nil {
		b.engine.scanObserver(b.name)
	}
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		if bytes.HasPrefix([]byte(key), prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		again, err := fn(vault.Key(key), append([]byte(nil), b.engine.buckets[b.name][key]...))
		if err != nil {
			return err
		}
		if !again {
			return nil
		}
	}
	return nil
}

func (b scriptedBucket) ReadKeyPageAfter(
	after vault.Key,
	limit int,
) (vault.BucketKeyPage, error) {
	if b.engine.keyPageError != nil {
		return vault.BucketKeyPage{}, b.engine.keyPageError
	}
	b.engine.keyPages.Add(1)
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	start := sort.Search(len(keys), func(position int) bool {
		return keys[position] > string(after)
	})
	end := min(start+limit, len(keys))
	page := make([]vault.Key, 0, end-start)
	for _, key := range keys[start:end] {
		page = append(page, vault.Key(key))
	}

	return vault.BucketKeyPage{Keys: page, More: end < len(keys)}, nil
}

func internalHashFor(base string) yagomodel.Hash {
	if len(base) >= yagomodel.HashLength {
		return yagomodel.Hash(base[:yagomodel.HashLength])
	}
	return yagomodel.Hash(base + internalHashFiller[len(base):])
}

func internalSeed(t testing.TB, hash, ip string) yagomodel.Seed {
	t.Helper()
	host, err := yagomodel.ParseHost(ip)
	if err != nil {
		t.Fatalf("ParseHost: %v", err)
	}
	return yagomodel.Seed{
		Hash: internalHashFor(hash),
		IP:   yagomodel.Some(host),
		Port: yagomodel.Some(yagomodel.Port(8090)),
	}
}

func openScriptedRoster(t *testing.T, reservoirCap, activeCap int) (*roster, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	opened, err := Open(
		t.Context(),
		storage,
		internalHashFor("local"),
		func() time.Time { return time.Unix(100, 0) },
		Capacity{Reservoir: reservoirCap, Active: activeCap},
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return opened.(*roster), engine
}

func corruptPeerRecord(t *testing.T, r *roster, engine *scriptedEngine, peer yagomodel.Hash) {
	t.Helper()
	engine.buckets[peersBucket][string(r.key(peer))] = []byte("bad")
}

func corruptPeerCount(t *testing.T, engine *scriptedEngine) {
	t.Helper()
	for name, bucket := range engine.buckets {
		if name != peersBucket && name != peerLifecyclesBucket &&
			name != peerLifecycleCleanupCursorBucket {
			bucket[string(peersBucket)] = []byte("bad")
			return
		}
	}
	t.Fatal("length bucket not found")
}

func TestOpenReturnsRegisterError(t *testing.T) {
	if _, err := Open(
		t.Context(), nil, internalHashFor("local"), time.Now,
		Capacity{Reservoir: 1, Active: 1},
	); err == nil {
		t.Fatal("expected register error")
	}
}

func TestOpenRemovesPersistedSelfForEveryClassification(t *testing.T) {
	for _, classification := range []yagomodel.PeerType{
		yagomodel.PeerJunior,
		yagomodel.PeerSenior,
	} {
		t.Run(classification.String(), func(t *testing.T) {
			engine := newScriptedEngine()
			firstStorage, err := vault.New(engine)
			if err != nil {
				t.Fatalf("vault.New first: %v", err)
			}
			first, err := Open(
				t.Context(), firstStorage, internalHashFor("former-local"), time.Now,
				Capacity{Reservoir: 8, Active: 4},
			)
			if err != nil {
				t.Fatalf("Open first: %v", err)
			}
			self := internalSeed(t, "current-local", "203.0.113.1")
			first.ObserveCaller(t.Context(), self, classification)
			if first.KnownPeerCount(t.Context()) != 1 {
				t.Fatal("persisted self fixture was not stored")
			}

			secondStorage, err := vault.New(engine)
			if err != nil {
				t.Fatalf("vault.New second: %v", err)
			}
			second, err := Open(
				t.Context(), secondStorage, self.Hash, time.Now,
				Capacity{Reservoir: 8, Active: 4},
			)
			if err != nil {
				t.Fatalf("Open second: %v", err)
			}
			if second.KnownPeerCount(t.Context()) != 0 {
				t.Fatalf("persisted %s self survived reopen", classification)
			}
		})
	}
}

func TestOpenFailsWhenPersistedSelfCannotBeRemoved(t *testing.T) {
	engine := newScriptedEngine()
	firstStorage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New first: %v", err)
	}
	first, err := Open(
		t.Context(), firstStorage, internalHashFor("former-local"), time.Now,
		Capacity{Reservoir: 8, Active: 4},
	)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	self := internalSeed(t, "current-local", "203.0.113.1")
	first.ObserveCaller(t.Context(), self, yagomodel.PeerSenior)

	deleteFailure := errors.New("delete failed")
	engine.deleteErrors[peersBucket] = deleteFailure
	secondStorage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New second: %v", err)
	}
	if _, err := Open(
		t.Context(), secondStorage, self.Hash, time.Now,
		Capacity{Reservoir: 8, Active: 4},
	); !errors.Is(
		err,
		deleteFailure,
	) {
		t.Fatalf("Open error = %v, want %v", err, deleteFailure)
	}
}

func TestSelfIsFilteredFromInjectedPersistentAndActiveState(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	self := internalSeed(t, "local", "203.0.113.1")
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.putRosterEntry(tx, r.key(self.Hash), rosterEntry{
			seed: self, lastSeen: time.Now(),
		})
	}); err != nil {
		t.Fatalf("inject persisted self: %v", err)
	}
	r.mu.Lock()
	r.active[self.Hash] = rosterEntry{seed: self}
	r.mu.Unlock()
	r.invalidateCandidateSnapshot()

	if r.KnownPeerCount(t.Context()) != 0 || r.ReachablePeerCount(t.Context()) != 0 {
		t.Fatalf(
			"injected self counts = known %d reachable %d",
			r.KnownPeerCount(t.Context()),
			r.ReachablePeerCount(t.Context()),
		)
	}
	if _, found := r.PeerByHash(t.Context(), self.Hash); found {
		t.Fatal("injected self resolved by hash")
	}
	if peers := r.ReachablePeers(t.Context()); len(peers) != 0 {
		t.Fatalf("injected reachable self = %#v", peers)
	}
	if peers := r.FreshestPeers(t.Context(), 8); len(peers) != 0 {
		t.Fatalf("injected candidate self = %#v", peers)
	}
	observations, known, reachable, err := r.PeerObservations(t.Context())
	if err != nil || len(observations) != 0 || known != 0 || reachable != 0 {
		t.Fatalf(
			"injected self observations = %#v known/reachable %d/%d error %v",
			observations,
			known,
			reachable,
			err,
		)
	}
	if _, found, err := r.PeerObservation(t.Context(), self.Hash); err != nil || found {
		t.Fatalf("injected self observation = found %v error %v", found, err)
	}
	selected := r.selectInactive(
		t.Context(),
		map[yagomodel.Hash]struct{}{},
		1,
		func(left, right rosterEntry) bool { return left.lastSeen.Before(right.lastSeen) },
	)
	if len(selected) != 0 {
		t.Fatalf("selected injected self = %#v", selected)
	}
}

func TestPrivateRosterMutationsRejectSelf(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	self := internalSeed(t, "local", "203.0.113.1")

	if stored, err := r.discoverOne(t.Context(), self); err != nil || stored {
		t.Fatalf("discoverOne self = stored %v error %v", stored, err)
	}
	if entry, _, found := r.touch(t.Context(), self.Hash); found || entry.seed.Hash != "" {
		t.Fatalf("touch self = %#v found %v", entry, found)
	}
	if _, err := r.persistCallerObservation(t.Context(), rosterEntry{seed: self}); err != nil {
		t.Fatalf("persist caller self: %v", err)
	}
	if _, err := r.persistResponderObservation(t.Context(), rosterEntry{seed: self}); err != nil {
		t.Fatalf("persist responder self: %v", err)
	}
}

func TestObserveResponderRejectsAddresslessAndRetainsJuniorInactive(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	addressless := yagomodel.Seed{Hash: internalHashFor("addressless")}
	r.ObserveResponder(t.Context(), addressless)

	junior := internalSeed(t, "junior", "203.0.113.2")
	junior.PeerType = yagomodel.Some(yagomodel.PeerJunior)
	r.ObserveResponder(t.Context(), junior)

	stored, found := r.PeerByHash(t.Context(), junior.Hash)
	classification, classified := stored.PeerType.Get()
	if !found || !classified || classification != yagomodel.PeerJunior ||
		r.KnownPeerCount(t.Context()) != 1 || r.ReachablePeerCount(t.Context()) != 0 {
		t.Fatalf(
			"junior responder = %#v found %v known/reachable %d/%d",
			stored,
			found,
			r.KnownPeerCount(t.Context()),
			r.ReachablePeerCount(t.Context()),
		)
	}
}

func TestObserveResponderDiscardsPersistenceFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	engine.putErrors[peersBucket] = errors.New("put failed")

	r.ObserveResponder(t.Context(), internalSeed(t, "peer", "203.0.113.1"))

	if r.KnownPeerCount(t.Context()) != 0 || r.ReachablePeerCount(t.Context()) != 0 {
		t.Fatalf(
			"failed responder counts = known %d reachable %d",
			r.KnownPeerCount(t.Context()),
			r.ReachablePeerCount(t.Context()),
		)
	}
}

func TestDiscoverKnownPeerNoops(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")

	r.Discover(t.Context(), peer)
	r.Discover(t.Context(), peer)

	if got := len(r.FreshestPeers(t.Context(), 4)); got != 1 {
		t.Fatalf("freshest peers = %d, want 1", got)
	}
}

func TestDiscoverLogsReadAndStoreErrors(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	corruptPeerRecord(t, r, engine, peer.Hash)

	r.Discover(t.Context(), peer)

	engine.buckets[peersBucket] = map[string][]byte{}
	engine.putErrors[peersBucket] = errors.New("put failed")
	r.Discover(t.Context(), internalSeed(t, "other", "203.0.113.2"))
}

func TestPeerByHashLogsReadError(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.Discover(t.Context(), peer)
	corruptPeerRecord(t, r, engine, peer.Hash)

	if _, ok := r.PeerByHash(t.Context(), peer.Hash); ok {
		t.Fatal("a corrupt peer record must not resolve by hash")
	}
}

func TestConfirmReachableLogsReadAndStoreErrors(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.Discover(t.Context(), peer)

	corruptPeerRecord(t, r, engine, peer.Hash)
	r.ConfirmReachable(t.Context(), peer.Hash)

	engine.buckets[peersBucket] = map[string][]byte{}
	engine.putErrors[peersBucket] = nil
	r.Discover(t.Context(), peer)
	engine.putErrors[peersBucket] = errors.New("put failed")
	r.ConfirmReachable(t.Context(), peer.Hash)
}

func TestConfirmReachableDoesNotExceedActiveCap(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 1)
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	r.Discover(t.Context(), first, second)

	r.ConfirmReachable(t.Context(), first.Hash)
	r.ConfirmReachable(t.Context(), second.Hash)

	if got := len(r.ReachablePeers(t.Context())); got != 1 {
		t.Fatalf("reachable peers = %d, want 1", got)
	}
}

func TestConfirmUnreachableLogsDeleteError(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.Discover(t.Context(), peer)
	engine.deleteErrors[peersBucket] = errors.New("delete failed")

	r.ConfirmUnreachable(t.Context(), peer.Hash)

	engine.deleteErrors[peersBucket] = nil
	corruptPeerRecord(t, r, engine, peer.Hash)
	r.ConfirmUnreachable(t.Context(), peer.Hash)
}

func TestRejectRemoteIndexLogsReadAndStoreErrors(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.Flags = yagomodel.Some(
		yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true),
	)
	r.Discover(t.Context(), peer)

	corruptPeerRecord(t, r, engine, peer.Hash)
	r.RejectRemoteIndex(t.Context(), peer)

	engine.buckets[peersBucket] = map[string][]byte{}
	engine.putErrors[peersBucket] = nil
	r.Discover(t.Context(), peer)
	engine.putErrors[peersBucket] = errors.New("put failed")
	r.RejectRemoteIndex(t.Context(), peer)
}

func TestRejectRemoteIndexNoopsForUnknownPeer(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	r.RejectRemoteIndex(t.Context(), internalSeed(t, "ghost", "203.0.113.1"))
}

func TestFreshestPeersLimitsActiveSnapshot(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	r.Discover(t.Context(), first, second)
	r.ConfirmReachable(t.Context(), first.Hash)
	r.ConfirmReachable(t.Context(), second.Hash)

	if got := len(r.FreshestPeers(t.Context(), 1)); got != 1 {
		t.Fatalf("freshest peers = %d, want 1", got)
	}
}

func TestReachablePeersSortNewestFirstWithHashTieBreak(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	now := time.Unix(100, 0)
	r.now = func() time.Time { return now }
	second := internalSeed(t, "BBBBBBBBBBBB", "203.0.113.2")
	first := internalSeed(t, "AAAAAAAAAAAA", "203.0.113.1")
	newest := internalSeed(t, "CCCCCCCCCCCC", "203.0.113.3")
	r.ObserveResponder(t.Context(), second)
	r.ObserveResponder(t.Context(), first)
	now = now.Add(time.Second)
	r.ObserveResponder(t.Context(), newest)

	peers := r.ReachablePeers(t.Context())
	if len(peers) != 3 ||
		peers[0].Hash != newest.Hash ||
		peers[1].Hash != first.Hash ||
		peers[2].Hash != second.Hash {
		t.Fatalf("reachable peers = %#v, want newest then hash order", peers)
	}
}

func TestEvictOverflowLogsDeleteError(t *testing.T) {
	r, engine := openScriptedRoster(t, 1, 4)
	r.Discover(t.Context(), internalSeed(t, "first", "203.0.113.1"))
	engine.deleteErrors[peersBucket] = errors.New("delete failed")

	r.Discover(t.Context(), internalSeed(t, "second", "203.0.113.2"))
}

func TestSelectInactiveLogsScanError(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	r.Discover(t.Context(), internalSeed(t, "peer", "203.0.113.1"))
	engine.scanErrors[peersBucket] = errors.New("scan failed")

	if got := r.FreshestPeers(t.Context(), 1); len(got) != 0 {
		t.Fatalf("freshest peers = %v, want empty on scan error", got)
	}
}

func TestSelectInactiveSkipsActiveAndDropsPastLimit(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	r.Discover(t.Context(), first, second)

	if got := r.selectInactive(
		t.Context(),
		nil,
		0,
		func(rosterEntry, rosterEntry) bool { return true },
	); got != nil {
		t.Fatalf("inactive selection = %v, want nil for zero limit", got)
	}

	got := r.selectInactive(
		t.Context(),
		map[yagomodel.Hash]struct{}{
			first.Hash:  {},
			second.Hash: {},
		},
		2,
		func(rosterEntry, rosterEntry) bool { return true },
	)
	if len(got) != 0 {
		t.Fatalf("inactive selection = %+v, want no inactive peers", got)
	}

	got = r.selectInactive(
		t.Context(),
		nil,
		1,
		func(rosterEntry, rosterEntry) bool { return false },
	)
	if len(got) != 1 {
		t.Fatalf("inactive selection = %d, want capped at 1", len(got))
	}
}

func TestSelectInactiveKeepsFreshestInOrder(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	now := time.Unix(100, 0)
	r.now = func() time.Time {
		now = now.Add(time.Second)

		return now
	}
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	third := internalSeed(t, "third", "203.0.113.3")
	r.Discover(t.Context(), first)
	r.Discover(t.Context(), second)
	r.Discover(t.Context(), third)

	got := r.selectInactive(t.Context(), nil, 2, func(left, right rosterEntry) bool {
		return left.lastSeen.After(right.lastSeen)
	})
	if len(got) != 2 || got[0].seed.Hash != third.Hash || got[1].seed.Hash != second.Hash {
		t.Fatalf("fresh inactive peers = %#v, want third then second", got)
	}
}

func TestPeerCountLogsLengthError(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	r.Discover(t.Context(), internalSeed(t, "peer", "203.0.113.1"))
	corruptPeerCount(t, engine)

	if got := r.peerCount(t.Context()); got != 0 {
		t.Fatalf("peerCount = %d, want 0 on length error", got)
	}
}

func TestRosterEntryDecodeRejectsBadRecords(t *testing.T) {
	if _, err := (rosterEntryCodec{}).Decode([]byte("bad")); err == nil {
		t.Fatal("expected short record error")
	}
	raw := make([]byte, lastSeenWidth, lastSeenWidth+10)
	binary.BigEndian.PutUint64(raw, uint64(time.Unix(1, 0).UnixNano()))
	raw = append(raw, []byte("not a seed")...)
	if _, err := (rosterEntryCodec{}).Decode(raw); err == nil {
		t.Fatal("expected seed parse error")
	}
}

func TestPeerObservationsReportLocalRecencyAndCounts(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	now := time.Unix(100, 0)
	r.now = func() time.Time {
		now = now.Add(time.Second)

		return now
	}
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	r.Discover(t.Context(), first)
	r.Discover(t.Context(), second)
	r.ConfirmReachable(t.Context(), first.Hash)

	observations, known, reachable, err := r.PeerObservations(t.Context())
	if err != nil {
		t.Fatalf("PeerObservations: %v", err)
	}
	if known != 2 || reachable != 1 || len(observations) != 2 {
		t.Fatalf("observations/counts = %d/%d/%d", len(observations), known, reachable)
	}
	if count, err := r.ObservedKnownPeerCount(t.Context()); err != nil || count != 2 {
		t.Fatalf("ObservedKnownPeerCount = %d/%v", count, err)
	}
	if observations[0].Seed.Hash != first.Hash ||
		observations[0].LastSeen != time.Unix(103, 0) ||
		observations[1].Seed.Hash != second.Hash ||
		observations[1].LastSeen != time.Unix(102, 0) {
		t.Fatalf("observations = %+v", observations)
	}

	observation, found, err := r.PeerObservation(t.Context(), first.Hash)
	if err != nil || !found || observation.Seed.Hash != first.Hash ||
		observation.LastSeen != time.Unix(103, 0) {
		t.Fatalf("PeerObservation = %+v/%v/%v", observation, found, err)
	}
	if _, found, err := r.PeerObservation(
		t.Context(), internalHashFor("ghost"),
	); err != nil || found {
		t.Fatalf("unknown PeerObservation = %v/%v", found, err)
	}
}

func TestPeerObservationsUseHashToOrderEqualRecency(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	r.Discover(
		t.Context(),
		internalSeed(t, "second", "203.0.113.2"),
		internalSeed(t, "first", "203.0.113.1"),
	)

	observations, _, _, err := r.PeerObservations(t.Context())
	if err != nil || len(observations) != 2 ||
		observations[0].Seed.Hash.String() >= observations[1].Seed.Hash.String() {
		t.Fatalf("equal-recency observations = %+v/%v", observations, err)
	}
}

func TestPeerObservationsSurfaceReadFailures(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.Discover(t.Context(), peer)

	engine.scanErrors[peersBucket] = errors.New("scan failed")
	if observations, known, reachable, err := r.PeerObservations(
		t.Context(),
	); err == nil || observations != nil ||
		known != 0 ||
		reachable != 0 {
		t.Fatalf("scan failure = %+v/%d/%d/%v", observations, known, reachable, err)
	}
	engine.scanErrors[peersBucket] = nil
	corruptPeerCount(t, engine)
	if count, err := r.ObservedKnownPeerCount(t.Context()); err == nil || count != 0 {
		t.Fatalf("corrupt ObservedKnownPeerCount = %d/%v", count, err)
	}
	corruptPeerRecord(t, r, engine, peer.Hash)
	if observation, found, err := r.PeerObservation(
		t.Context(),
		peer.Hash,
	); err == nil || found ||
		observation.Seed.Hash != "" ||
		!observation.LastSeen.IsZero() {
		t.Fatalf("lookup failure = %+v/%v/%v", observation, found, err)
	}
}

func TestPeerObservationsSurfaceCanceledScan(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	r.Discover(t.Context(), internalSeed(t, "peer", "203.0.113.1"))
	ctx, cancel := context.WithCancel(t.Context())
	engine.scanObserver = func(name vault.Name) {
		if name == peersBucket {
			cancel()
		}
	}

	if _, _, _, err := r.PeerObservations(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("PeerObservations error = %v, want context canceled", err)
	}
}

func TestPeerObservationsSurfaceClosedVault(t *testing.T) {
	storage, err := vault.New(newScriptedEngine())
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	opened, err := Open(
		t.Context(), storage, internalHashFor("local"), time.Now,
		Capacity{Reservoir: 8, Active: 4},
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reader := opened.(ObservationReader)
	if _, _, _, err := reader.PeerObservations(t.Context()); err == nil {
		t.Fatal("PeerObservations on a closed vault succeeded")
	}
	if _, _, err := reader.PeerObservation(
		t.Context(), internalHashFor("peer"),
	); err == nil {
		t.Fatal("PeerObservation on a closed vault succeeded")
	}
	if count, err := opened.(*roster).ObservedKnownPeerCount(
		t.Context(),
	); err == nil ||
		count != 0 {
		t.Fatalf("ObservedKnownPeerCount on a closed vault = %d/%v", count, err)
	}
}
