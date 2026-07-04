package peerroster

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const internalHashFiller = "AAAAAAAAAAAA"

type scriptedEngine struct {
	buckets      map[vault.Name]map[string][]byte
	putErrors    map[vault.Name]error
	deleteErrors map[vault.Name]error
	scanErrors   map[vault.Name]error
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{
		buckets:      map[vault.Name]map[string][]byte{},
		putErrors:    map[vault.Name]error{},
		deleteErrors: map[vault.Name]error{},
		scanErrors:   map[vault.Name]error{},
	}
}

func (e *scriptedEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return fn(scriptedTxn{engine: e, writable: true})
}

func (e *scriptedEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return fn(scriptedTxn{engine: e})
}

func (e *scriptedEngine) Provision(name vault.Name) error {
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
		storage,
		func() time.Time { return time.Unix(100, 0) },
		reservoirCap,
		activeCap,
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
		if name != peersBucket {
			bucket[string(peersBucket)] = []byte("bad")
			return
		}
	}
	t.Fatal("length bucket not found")
}

func TestOpenReturnsRegisterError(t *testing.T) {
	if _, err := Open(nil, time.Now, 1, 1); err == nil {
		t.Fatal("expected register error")
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
