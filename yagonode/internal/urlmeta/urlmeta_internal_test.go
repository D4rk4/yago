package urlmeta

import (
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

type scriptedEngine struct {
	buckets      map[vault.Name]map[string][]byte
	putErrors    map[vault.Name]error
	deleteErrors map[vault.Name]error
	updateErr    error
	replayNext   bool
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{
		buckets:      map[vault.Name]map[string][]byte{},
		putErrors:    map[vault.Name]error{},
		deleteErrors: map[vault.Name]error{},
	}
}

func (e *scriptedEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if e.updateErr != nil {
		return e.updateErr
	}
	if e.replayNext {
		e.replayNext = false
		before := cloneScriptedURLBuckets(e.buckets)
		if err := fn(scriptedTxn{engine: e, writable: true}); err != nil {
			return err
		}
		e.buckets = before
	}
	return fn(scriptedTxn{engine: e, writable: true})
}

func cloneScriptedURLBuckets(
	source map[vault.Name]map[string][]byte,
) map[vault.Name]map[string][]byte {
	cloned := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		cloned[name] = make(map[string][]byte, len(bucket))
		for key, value := range bucket {
			cloned[name][key] = append([]byte(nil), value...)
		}
	}

	return cloned
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

func openScriptedModule(
	t *testing.T,
	watchers ...URLMetadataObserver,
) (*vault.Vault, urlPorts, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	directory, evictor, receiver, err := Open(storage, watchers...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return storage, urlPorts{Directory: directory, Evictor: evictor, Receiver: receiver}, engine
}

func corruptLength(t *testing.T, engine *scriptedEngine) {
	t.Helper()
	for name, bucket := range engine.buckets {
		if name != bucketName {
			bucket[string(bucketName)] = []byte("bad")
			return
		}
	}
	t.Fatal("length bucket not found")
}

func compressedURLMetadata(raw string) []byte {
	var buf bytes.Buffer
	buf.WriteByte(storedURLMetadataFormatV1)
	writer, _ := flate.NewWriterDict(&buf, flate.BestCompression, urlMetadataDictionary)
	_, _ = writer.Write([]byte(raw))
	_ = writer.Close()
	return buf.Bytes()
}

func TestOpenReturnsRegisterError(t *testing.T) {
	if _, _, _, err := Open(nil); err == nil {
		t.Fatal("expected register error")
	}
}

func TestURLMetadataCodecDecodeReturnsError(t *testing.T) {
	if _, err := (uriMetadataCodec{}).Decode(nil); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDecodeStoredURLMetadataRejectsPropertyAndInflatedParseErrors(t *testing.T) {
	if _, err := decodeStoredURLMetadata([]byte("not a row")); err == nil {
		t.Fatal("expected property form parse error")
	}
	if _, err := decodeStoredURLMetadata(compressedURLMetadata("not a row")); err == nil {
		t.Fatal("expected inflated parse error")
	}
}

func TestIntakeReturnsCapacityCheckError(t *testing.T) {
	v, module := openObservedModule(t)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := module.Receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{urlRow(t, "a")},
	); err == nil {
		t.Fatal("expected capacity check error")
	}
}

func TestEmptyIntakeDoesNotAccessClosedVault(t *testing.T) {
	v, module, _ := openScriptedModule(t)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	receipt, err := module.Receiver.Receive(t.Context(), nil)
	if err != nil || receipt.Busy || receipt.Double != 0 || len(receipt.ErrorURL) != 0 {
		t.Fatalf("empty receipt = %+v/%v", receipt, err)
	}
}

func TestIntakeMapsUpdateCapacityAndErrors(t *testing.T) {
	_, module, engine := openScriptedModule(t)
	engine.updateErr = vault.ErrAtCapacity
	receipt, err := module.Receiver.Receive(t.Context(), []yagomodel.URIMetadataRow{urlRow(t, "a")})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Busy {
		t.Fatalf("receipt = %+v, want busy", receipt)
	}

	_, module, engine = openScriptedModule(t)
	engine.updateErr = errors.New("update failed")
	if _, err := module.Receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{urlRow(t, "a")},
	); err == nil {
		t.Fatal("expected update error")
	}
}

func TestIntakeMapsContextErrorBeforeCommitToBusy(t *testing.T) {
	module := openModule(t, 0)
	ctx := &errAfterContext{Context: context.Background(), remaining: 2, err: context.Canceled}

	receipt, err := module.Receiver.Receive(
		ctx,
		[]yagomodel.URIMetadataRow{urlRow(t, "a")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Busy {
		t.Fatalf("receipt = %+v, want busy", receipt)
	}
}

func TestIntakeMapsCanceledCapacityCheckToBusy(t *testing.T) {
	module := openModule(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	receipt, err := module.Receiver.Receive(
		ctx,
		[]yagomodel.URIMetadataRow{urlRow(t, "a")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Busy {
		t.Fatalf("receipt = %+v, want busy", receipt)
	}
}

func TestIntakeDiscardsBadRowsAndReportsStoreFailures(t *testing.T) {
	module := openModule(t, 0)
	receipt, err := module.Receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{{Properties: map[string]string{}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.ErrorURL) != 0 || receipt.Double != 0 {
		t.Fatalf("receipt = %+v, want discarded row without error URLs", receipt)
	}

	_, module, engine := openScriptedModule(t)
	engine.putErrors[bucketName] = errors.New("put failed")
	row := urlRow(t, "a")
	receipt, err = module.Receiver.Receive(t.Context(), []yagomodel.URIMetadataRow{row})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.ErrorURL) != 1 || receipt.ErrorURL[0] != rowHash(t, row) {
		t.Fatalf("ErrorURL = %v, want stored hash", receipt.ErrorURL)
	}
}

func TestIntakeReturnsReadError(t *testing.T) {
	_, module, engine := openScriptedModule(t)
	row := urlRow(t, "a")
	engine.buckets[bucketName][rowHash(t, row).String()] = []byte("bad")

	if _, err := module.Receiver.Receive(t.Context(), []yagomodel.URIMetadataRow{row}); err == nil {
		t.Fatal("expected intake read error")
	}
}

func TestDirectoryReturnsReadErrors(t *testing.T) {
	_, module, engine := openScriptedModule(t)
	row := urlRow(t, "a")
	hash := rowHash(t, row)
	if _, err := module.Receiver.Receive(t.Context(), []yagomodel.URIMetadataRow{row}); err != nil {
		t.Fatal(err)
	}
	engine.buckets[bucketName][hash.String()] = []byte("bad")

	if _, err := module.Directory.RowsByHash(t.Context(), []yagomodel.Hash{hash}); err == nil {
		t.Fatal("expected RowsByHash read error")
	}
	if _, err := module.Directory.MissingURLs(t.Context(), []yagomodel.Hash{hash}); err == nil {
		t.Fatal("expected MissingURLs read error")
	}
}

func TestDirectoryHandlesMissingRowsAndViewErrors(t *testing.T) {
	v, module := openObservedModule(t)
	rows, err := module.Directory.RowsByHash(
		t.Context(),
		[]yagomodel.Hash{yagomodel.WordHash("absent")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %v, want empty for missing hash", rows)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Directory.RowsByHash(
		t.Context(),
		[]yagomodel.Hash{yagomodel.WordHash("absent")},
	); err == nil {
		t.Fatal("expected RowsByHash view error")
	}
	if _, err := module.Directory.MissingURLs(
		t.Context(),
		[]yagomodel.Hash{yagomodel.WordHash("absent")},
	); err == nil {
		t.Fatal("expected MissingURLs view error")
	}
}

func TestDirectoryVisitsStoredURLMetadataRows(t *testing.T) {
	module := openModule(t, 0)
	first := urlRow(t, "a")
	second := urlRow(t, "b")
	if _, err := module.Receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{second, first},
	); err != nil {
		t.Fatal(err)
	}

	rows := storedMetadataRows(t, module.Directory)
	var visited []yagomodel.Hash
	if err := rows.StoredURLMetadataRows(
		t.Context(),
		func(row yagomodel.URIMetadataRow) (bool, error) {
			hash, err := row.URLHash()
			if err != nil {
				t.Fatalf("URLHash: %v", err)
			}
			visited = append(visited, hash.Hash())

			return true, nil
		},
	); err != nil {
		t.Fatal(err)
	}

	want := []yagomodel.Hash{rowHash(t, first), rowHash(t, second)}
	if !slices.Equal(visited, want) {
		t.Fatalf("visited = %v, want %v", visited, want)
	}
}

func TestDirectoryStopsStoredURLMetadataRows(t *testing.T) {
	module := openModule(t, 0)
	if _, err := module.Receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{urlRow(t, "a"), urlRow(t, "b")},
	); err != nil {
		t.Fatal(err)
	}

	rows := storedMetadataRows(t, module.Directory)
	var visits int
	if err := rows.StoredURLMetadataRows(
		t.Context(),
		func(yagomodel.URIMetadataRow) (bool, error) {
			visits++

			return false, nil
		},
	); err != nil {
		t.Fatal(err)
	}

	if visits != 1 {
		t.Fatalf("visits = %d, want 1", visits)
	}
}

func TestDirectoryStoredURLMetadataRowsReturnsErrors(t *testing.T) {
	t.Run("visitor", func(t *testing.T) {
		module := openModule(t, 0)
		if _, err := module.Receiver.Receive(
			t.Context(),
			[]yagomodel.URIMetadataRow{urlRow(t, "a")},
		); err != nil {
			t.Fatal(err)
		}
		sentinel := errors.New("visitor failed")
		rows := storedMetadataRows(t, module.Directory)
		err := rows.StoredURLMetadataRows(
			t.Context(),
			func(yagomodel.URIMetadataRow) (bool, error) {
				return false, sentinel
			},
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("StoredURLMetadataRows error = %v, want %v", err, sentinel)
		}
	})

	t.Run("context", func(t *testing.T) {
		_, module, _ := openScriptedModule(t)
		if _, err := module.Receiver.Receive(
			t.Context(),
			[]yagomodel.URIMetadataRow{urlRow(t, "a")},
		); err != nil {
			t.Fatal(err)
		}
		ctx := &errAfterContext{Context: context.Background(), remaining: 2, err: context.Canceled}
		rows := storedMetadataRows(t, module.Directory)
		err := rows.StoredURLMetadataRows(ctx, func(yagomodel.URIMetadataRow) (bool, error) {
			return true, nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("StoredURLMetadataRows error = %v, want context.Canceled", err)
		}
	})

	t.Run("decode", func(t *testing.T) {
		_, module, engine := openScriptedModule(t)
		row := urlRow(t, "a")
		if _, err := module.Receiver.Receive(
			t.Context(),
			[]yagomodel.URIMetadataRow{row},
		); err != nil {
			t.Fatal(err)
		}
		engine.buckets[bucketName][rowHash(t, row).String()] = []byte("bad")
		rows := storedMetadataRows(t, module.Directory)
		err := rows.StoredURLMetadataRows(
			t.Context(),
			func(yagomodel.URIMetadataRow) (bool, error) {
				return true, nil
			},
		)
		if err == nil {
			t.Fatal("expected decode error")
		}
	})
}

func TestCountReturnsLengthError(t *testing.T) {
	_, module, engine := openScriptedModule(t)
	if _, err := module.Receiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{urlRow(t, "a")},
	); err != nil {
		t.Fatal(err)
	}
	corruptLength(t, engine)
	if _, err := module.Directory.Count(t.Context()); err == nil {
		t.Fatal("expected count error")
	}
}

func TestPurgeReportsDeleteErrorAndAbsentRows(t *testing.T) {
	storage, module, engine := openScriptedModule(t)
	row := urlRow(t, "a")
	hash := rowHash(t, row)
	if _, err := module.Receiver.Receive(t.Context(), []yagomodel.URIMetadataRow{row}); err != nil {
		t.Fatal(err)
	}
	engine.deleteErrors[bucketName] = errors.New("delete failed")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := module.Evictor.Purge(t.Context(), tx, []yagomodel.Hash{hash})
		if err != nil {
			return fmt.Errorf("purge url: %w", err)
		}

		return nil
	}); err == nil {
		t.Fatal("expected purge delete error")
	}

	engine.deleteErrors[bucketName] = nil
	var result PurgeResult
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		var err error
		result, err = module.Evictor.Purge(
			t.Context(),
			tx,
			[]yagomodel.Hash{yagomodel.WordHash("absent")},
		)
		if err != nil {
			return fmt.Errorf("purge url: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if result.URLsDeleted != 0 {
		t.Fatalf("URLsDeleted = %d, want 0", result.URLsDeleted)
	}
}

func storedMetadataRows(t *testing.T, directory URLDirectory) StoredURLMetadataRows {
	t.Helper()

	rows, ok := directory.(StoredURLMetadataRows)
	if !ok {
		t.Fatal("directory does not expose stored URL metadata rows")
	}

	return rows
}

type failingURLReceiver struct{}

func (failingURLReceiver) Receive(context.Context, []yagomodel.URIMetadataRow) (Receipt, error) {
	return Receipt{}, errors.New("receive failed")
}

type busyURLReceiver struct{}

func (busyURLReceiver) Receive(context.Context, []yagomodel.URIMetadataRow) (Receipt, error) {
	return Receipt{
		Busy:     true,
		Double:   1,
		ErrorURL: []yagomodel.Hash{yagomodel.WordHash("bad")},
	}, nil
}

func TestTransferURLReportsReceiveErrorAndBusy(t *testing.T) {
	_, err := (transferURLEndpoint{identity: localIdentity(), intake: failingURLReceiver{}, accept: true}).Serve(
		t.Context(),
		yagoproto.TransferURLRequest{NetworkName: "freeworld", YouAre: localIdentity().Hash},
	)
	if err == nil {
		t.Fatal("expected receive error")
	}

	resp, err := (transferURLEndpoint{identity: localIdentity(), intake: busyURLReceiver{}, accept: true}).Serve(
		t.Context(),
		yagoproto.TransferURLRequest{NetworkName: "freeworld", YouAre: localIdentity().Hash},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Result != yagoproto.ResultErrorNotGranted {
		t.Fatalf("Result = %q, want not granted", resp.Result)
	}
	if resp.Double != 1 || len(resp.ErrorURL) != 1 {
		t.Fatalf("response = %+v, want receipt fields copied", resp)
	}
}

type urlWireStatus struct{}

func (urlWireStatus) Version(context.Context) string { return "1.940" }
func (urlWireStatus) Uptime(context.Context) int     { return 42 }

type okURLReceiver struct{}

func (okURLReceiver) Receive(context.Context, []yagomodel.URIMetadataRow) (Receipt, error) {
	return Receipt{}, nil
}

func TestMountTransferURLServesRoute(t *testing.T) {
	mux := http.NewServeMux()
	MountTransferURL(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(urlWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		localIdentity(),
		okURLReceiver{},
		nil,
		true,
	)
	req := yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		Iam:         yagomodel.WordHash("sender"),
		URLCount:    1,
		URLs:        []yagomodel.URIMetadataRow{urlRow(t, "a")},
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathTransferURL+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMountTransferURLRejectsExtremeCountBeforeReceiver(t *testing.T) {
	mux := http.NewServeMux()
	MountTransferURL(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(urlWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		localIdentity(),
		failingURLReceiver{},
		nil,
		true,
	)
	req := yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		Iam:         yagomodel.WordHash("sender"),
		URLCount:    math.MaxInt,
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathTransferURL+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMountTransferURLMapsUncommittedDeadlineToNotGranted(t *testing.T) {
	_, module, engine := openScriptedModule(t)
	engine.updateErr = context.DeadlineExceeded
	mux := http.NewServeMux()
	MountTransferURL(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(urlWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		localIdentity(),
		module.Receiver,
		nil,
		true,
	)
	req := yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		Iam:         yagomodel.WordHash("sender"),
		URLCount:    1,
		URLs:        []yagomodel.URIMetadataRow{urlRow(t, "a")},
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathTransferURL+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	message, err := yagomodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	response, err := yagoproto.ParseTransferURLResponse(message)
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != yagoproto.ResultErrorNotGranted {
		t.Fatalf("response = %+v, want error_not_granted", response)
	}
	count, err := module.Directory.Count(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("URL count = %d, want no committed row", count)
	}
}
