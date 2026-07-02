package rwi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
	"github.com/D4rk4/yago/yacynode/internal/vault"
	"github.com/D4rk4/yago/yacyproto"
)

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

type fakeURLDirectory struct {
	err error
}

func (d fakeURLDirectory) RowsByHash(
	context.Context,
	[]yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	return nil, nil
}

func (d fakeURLDirectory) MissingURLs(
	_ context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	if d.err != nil {
		return nil, d.err
	}
	return hashes, nil
}

func (d fakeURLDirectory) Count(context.Context) (int, error) { return 0, nil }

type failingObserver struct {
	storeErr error
	purgeErr error
}

func (o failingObserver) PostingStored(*vault.Txn, yacymodel.Hash, yacymodel.Hash) error {
	return o.storeErr
}

func (o failingObserver) PostingPurged(*vault.Txn, yacymodel.Hash, yacymodel.Hash) error {
	return o.purgeErr
}

type errAfterContext struct {
	context.Context
	remaining int
	err       error
}

func (c *errAfterContext) Err() error {
	if c.remaining > 0 {
		c.remaining--
		return nil
	}
	return c.err
}

func openScriptedRWI(
	t *testing.T,
	urls urlmeta.URLDirectory,
	observers ...PostingObserver,
) (*vault.Vault, PostingIndex, PostingReceiver, PostingPurger, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	index, receiver, purger, err := Open(
		storage,
		urls,
		Config{BatchCap: 10, PauseSeconds: 5},
		observers...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return storage, index, receiver, purger, engine
}

func corruptLength(t *testing.T, engine *scriptedEngine) {
	t.Helper()
	for name, bucket := range engine.buckets {
		if name != postingsBucket {
			bucket[string(postingsBucket)] = []byte("bad")
			return
		}
	}
	t.Fatal("length bucket not found")
}

func TestOpenReturnsRegisterError(t *testing.T) {
	if _, _, _, err := Open(nil, fakeURLDirectory{}, Config{}); err == nil {
		t.Fatal("expected register error")
	}
}

func TestPostingCodecDecodeReturnsError(t *testing.T) {
	if _, err := (postingCodec{}).Decode(nil); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestIntakeReturnsCapacityCheckError(t *testing.T) {
	h := openHarness(t, 0, 100)
	if err := h.vault.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := h.rwi.Receiver.Receive(
		t.Context(),
		[]yacymodel.RWIPosting{posting("w1", "u1")},
	); err == nil {
		t.Fatal("expected capacity check error")
	}
}

func TestIntakeReturnsContextErrorInsideBatch(t *testing.T) {
	h := openHarness(t, 0, 100)
	ctx := &errAfterContext{
		Context:   context.Background(),
		remaining: 2,
		err:       context.Canceled,
	}

	if _, err := h.rwi.Receiver.Receive(
		ctx,
		[]yacymodel.RWIPosting{posting("w1", "u1")},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Receive error = %v, want context.Canceled", err)
	}
}

func TestIntakeReturnsBadPostingError(t *testing.T) {
	h := openHarness(t, 0, 100)
	_, err := h.rwi.Receiver.Receive(t.Context(), []yacymodel.RWIPosting{
		{WordHash: yacymodel.WordHash("w1"), Properties: map[string]string{}},
	})
	if err == nil {
		t.Fatal("expected bad posting error")
	}
}

func TestIntakeMapsVaultCapacityDuringStore(t *testing.T) {
	_, _, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	engine.putErrors[postingsBucket] = vault.ErrAtCapacity

	receipt, err := receiver.Receive(t.Context(), []yacymodel.RWIPosting{posting("w1", "u1")})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Busy || receipt.Pause != 5 {
		t.Fatalf("receipt = %+v, want busy pause 5", receipt)
	}
}

func TestIntakeReturnsStoreAndObserverErrors(t *testing.T) {
	_, _, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	engine.putErrors[postingsBucket] = errors.New("put failed")
	if _, err := receiver.Receive(
		t.Context(),
		[]yacymodel.RWIPosting{posting("w1", "u1")},
	); err == nil {
		t.Fatal("expected store error")
	}

	_, _, receiver, _, _ = openScriptedRWI(
		t,
		fakeURLDirectory{},
		failingObserver{storeErr: errors.New("observer failed")},
	)
	if _, err := receiver.Receive(
		t.Context(),
		[]yacymodel.RWIPosting{posting("w1", "u1")},
	); err == nil {
		t.Fatal("expected observer error")
	}
}

func TestIntakeReturnsMissingURLError(t *testing.T) {
	_, _, receiver, _, _ := openScriptedRWI(t, fakeURLDirectory{err: errors.New("missing failed")})

	if _, err := receiver.Receive(
		t.Context(),
		[]yacymodel.RWIPosting{posting("w1", "u1")},
	); err == nil {
		t.Fatal("expected missing URL error")
	}
}

func TestPurgePostingReturnsDeleteAndObserverErrors(t *testing.T) {
	storage, _, receiver, purger, engine := openScriptedRWI(t, fakeURLDirectory{})
	entry := posting("w1", "u1")
	if _, err := receiver.Receive(t.Context(), []yacymodel.RWIPosting{entry}); err != nil {
		t.Fatal(err)
	}
	engine.deleteErrors[postingsBucket] = errors.New("delete failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := purger.PurgePosting(tx, yacymodel.WordHash("w1"), referencedHash(t, entry))
		if err != nil {
			return fmt.Errorf("purge posting: %w", err)
		}

		return nil
	}); err == nil {
		t.Fatal("expected delete error")
	}

	storage, _, receiver, purger, _ = openScriptedRWI(
		t,
		fakeURLDirectory{},
		failingObserver{purgeErr: errors.New("observer failed")},
	)
	if _, err := receiver.Receive(t.Context(), []yacymodel.RWIPosting{entry}); err != nil {
		t.Fatal(err)
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := purger.PurgePosting(tx, yacymodel.WordHash("w1"), referencedHash(t, entry))
		if err != nil {
			return fmt.Errorf("purge posting: %w", err)
		}

		return nil
	}); err == nil {
		t.Fatal("expected observer error")
	}
}

func TestPurgePostingReportsAbsentPosting(t *testing.T) {
	storage, _, _, purger, _ := openScriptedRWI(t, fakeURLDirectory{})
	var deleted bool
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		var err error
		deleted, err = purger.PurgePosting(tx, yacymodel.WordHash("w1"), yacymodel.WordHash("u1"))
		if err != nil {
			return fmt.Errorf("purge posting: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("deleted = true, want false for absent posting")
	}
}

func TestScanWordReturnsContextVisitorAndStorageErrors(t *testing.T) {
	h := openHarness(t, 0, 100)
	if _, err := h.rwi.Receiver.Receive(
		t.Context(),
		[]yacymodel.RWIPosting{posting("w1", "u1")},
	); err != nil {
		t.Fatal(err)
	}
	ctx := &errAfterContext{Context: context.Background(), remaining: 2, err: context.Canceled}
	if err := h.rwi.Index.ScanWord(
		ctx,
		yacymodel.WordHash("w1"),
		func(yacymodel.RWIPosting) (bool, error) {
			return true, nil
		},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("ScanWord error = %v, want context.Canceled", err)
	}
	if err := h.rwi.Index.ScanWord(
		t.Context(),
		yacymodel.WordHash("w1"),
		func(yacymodel.RWIPosting) (bool, error) {
			return false, errors.New("visitor failed")
		},
	); err == nil {
		t.Fatal("expected visitor error")
	}

	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(
		t.Context(),
		[]yacymodel.RWIPosting{posting("w1", "u1")},
	); err != nil {
		t.Fatal(err)
	}
	engine.scanErrors[postingsBucket] = errors.New("scan failed")
	if err := index.ScanWord(
		t.Context(),
		yacymodel.WordHash("w1"),
		func(yacymodel.RWIPosting) (bool, error) {
			return true, nil
		},
	); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestRWICountReturnsLengthError(t *testing.T) {
	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(
		t.Context(),
		[]yacymodel.RWIPosting{posting("w1", "u1")},
	); err != nil {
		t.Fatal(err)
	}
	corruptLength(t, engine)

	if _, err := index.RWICount(t.Context()); err == nil {
		t.Fatal("expected length error")
	}
}

type failingPostingReceiver struct{}

func (failingPostingReceiver) Receive(context.Context, []yacymodel.RWIPosting) (Receipt, error) {
	return Receipt{}, errors.New("receive failed")
}

func TestTransferRWIReportsReceiveError(t *testing.T) {
	_, err := (transferRWIEndpoint{
		identity: localIdentity(),
		intake:   failingPostingReceiver{},
	}).Serve(t.Context(), yacyproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
	})
	if err == nil {
		t.Fatal("expected receive error")
	}
}

type rwiWireStatus struct{}

func (rwiWireStatus) Version(context.Context) string { return "1.940" }
func (rwiWireStatus) Uptime(context.Context) int     { return 42 }

func TestMountTransferRWIServesRoute(t *testing.T) {
	mux := http.NewServeMux()
	MountTransferRWI(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(rwiWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		localIdentity(),
		fakePostingReceiver{},
	)
	req := yacyproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		Iam:         yacymodel.WordHash("sender"),
		Indexes:     []yacymodel.RWIPosting{posting("w1", "u1")},
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferRWI+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

type fakePostingReceiver struct{}

func (fakePostingReceiver) Receive(context.Context, []yacymodel.RWIPosting) (Receipt, error) {
	return Receipt{}, nil
}

func TestDecodeStoredPostingRejectsPropertyFormError(t *testing.T) {
	if _, err := decodeStoredPosting("ABCDEFGHIJKL", []byte("not a posting")); err == nil {
		t.Fatal("expected property form parse error")
	}
}

func TestDecodeStoredPostingColumnEdgeErrors(t *testing.T) {
	if _, err := decodeStoredPostingColumn(
		bytes.NewReader(nil),
		yacymodel.ColLanguage,
	); err == nil {
		t.Fatal("expected language length error")
	}
	if _, err := decodeStoredPostingColumn(
		bytes.NewReader([]byte{1}),
		yacymodel.ColFlags,
	); err == nil {
		t.Fatal("expected flags length error")
	}
	if _, err := decodeStoredPostingColumn(
		bytes.NewReader(nil),
		yacymodel.ColHitCount,
	); err == nil {
		t.Fatal("expected cardinal error")
	}
}

func TestDecodeStoredPostingExtrasEdgeErrors(t *testing.T) {
	if err := decodeStoredPostingExtras(bytes.NewReader(nil), map[string]string{}); err == nil {
		t.Fatal("expected extras count error")
	}
	if err := decodeStoredPostingExtras(
		bytes.NewReader([]byte{2}),
		map[string]string{},
	); err == nil {
		t.Fatal("expected extras count overflow")
	}
	if err := decodeStoredPostingExtras(
		bytes.NewReader([]byte{1, 2}),
		map[string]string{},
	); err == nil {
		t.Fatal("expected extras key error")
	}
	var data bytes.Buffer
	data.WriteByte(1)
	writeLengthPrefixed(&data, []byte("key"))
	if err := decodeStoredPostingExtras(
		bytes.NewReader(data.Bytes()),
		map[string]string{},
	); err == nil {
		t.Fatal("expected extras value error")
	}
}

func TestDecodeStoredPostingRejectsColumnError(t *testing.T) {
	var data bytes.Buffer
	data.WriteByte(storedPostingFormatV1)
	var mask [4]byte
	binary.LittleEndian.PutUint32(mask[:], 1<<uint(storedPostingColumnIndex[yacymodel.ColLanguage]))
	data.Write(mask[:])
	data.WriteByte(2)

	if _, err := decodeStoredPosting("ABCDEFGHIJKL", data.Bytes()); err == nil {
		t.Fatal("expected column decode error")
	}
}
