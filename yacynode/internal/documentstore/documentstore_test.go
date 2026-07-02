package documentstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/memvault"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type scriptedDocumentEngine struct {
	buckets   map[vault.Name]map[string][]byte
	putErrors map[vault.Name]error
}

func newScriptedDocumentEngine() *scriptedDocumentEngine {
	return &scriptedDocumentEngine{
		buckets:   map[vault.Name]map[string][]byte{},
		putErrors: map[vault.Name]error{},
	}
}

func (e *scriptedDocumentEngine) Provision(name vault.Name) error {
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}
	return nil
}

func (e *scriptedDocumentEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return fn(scriptedDocumentTxn{engine: e, writable: true})
}

func (e *scriptedDocumentEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return fn(scriptedDocumentTxn{engine: e})
}

func (e *scriptedDocumentEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *scriptedDocumentEngine) QuotaBytes() int64                        { return 0 }
func (e *scriptedDocumentEngine) Close() error                             { return nil }

type scriptedDocumentTxn struct {
	engine   *scriptedDocumentEngine
	writable bool
}

func (t scriptedDocumentTxn) Bucket(name vault.Name) vault.EngineBucket {
	return scriptedDocumentBucket{engine: t.engine, name: name}
}

func (t scriptedDocumentTxn) Writable() bool { return t.writable }

type scriptedDocumentBucket struct {
	engine *scriptedDocumentEngine
	name   vault.Name
}

func (b scriptedDocumentBucket) Get(key vault.Key) []byte {
	raw := b.engine.buckets[b.name][string(key)]
	if raw == nil {
		return nil
	}
	return append([]byte(nil), raw...)
}

func (b scriptedDocumentBucket) Put(key vault.Key, raw []byte) error {
	if err := b.engine.putErrors[b.name]; err != nil {
		return err
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), raw...)
	return nil
}

func (b scriptedDocumentBucket) Delete(key vault.Key) error {
	delete(b.engine.buckets[b.name], string(key))
	return nil
}

func (b scriptedDocumentBucket) Scan(
	prefix vault.Key,
	fn func(vault.Key, []byte) (bool, error),
) error {
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		if strings.HasPrefix(key, string(prefix)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
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

func openDocuments(t *testing.T) (DocumentDirectory, DocumentReceiver) {
	t.Helper()

	_, directory, receiver := openDocumentsWithVault(t, 0)
	return directory, receiver
}

func openDocumentsWithVault(
	t *testing.T,
	quotaBytes int64,
) (*vault.Vault, DocumentDirectory, DocumentReceiver) {
	t.Helper()

	v, err := memvault.Open(quotaBytes)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	directory, receiver, err := Open(v)
	if err != nil {
		t.Fatalf("open documents: %v", err)
	}
	return v, directory, receiver
}

func openScriptedDocuments(
	t *testing.T,
) (DocumentDirectory, DocumentReceiver, *scriptedDocumentEngine) {
	t.Helper()

	engine := newScriptedDocumentEngine()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	directory, receiver, err := Open(v)
	if err != nil {
		t.Fatalf("open documents: %v", err)
	}
	return directory, receiver, engine
}

func TestReceiveStoresDocument(t *testing.T) {
	directory, receiver := openDocuments(t)
	doc := Document{
		CanonicalURL:  "https://example.org/",
		NormalizedURL: "https://example.org/",
		Title:         "Example",
		Headings:      []string{"Heading"},
		ExtractedText: "body text",
		Outlinks:      []string{"https://example.org/a"},
		Metadata:      map[string]string{"url_hash": "abc"},
	}

	receipt, err := receiver.Receive(context.Background(), []Document{doc})
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if receipt.Stored != 1 || receipt.Updated != 0 || receipt.Rejected != 0 {
		t.Fatalf("receipt = %#v", receipt)
	}

	got, ok, err := directory.Document(context.Background(), doc.NormalizedURL)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if !ok {
		t.Fatal("document missing")
	}
	if got.Title != doc.Title || got.ExtractedText != doc.ExtractedText {
		t.Fatalf("document = %#v", got)
	}
}

func TestReceiveDefaultsCanonicalURLAndCopiesValues(t *testing.T) {
	directory, receiver := openDocuments(t)
	doc := Document{
		NormalizedURL: "https://example.org/",
		Headings:      []string{"Heading"},
		Outlinks:      []string{"https://example.org/a"},
		Inlinks:       []AnchorText{{URL: "https://example.org/from", Text: "anchor"}},
		Metadata:      map[string]string{"url_hash": "abc"},
	}

	if _, err := receiver.Receive(context.Background(), []Document{doc}); err != nil {
		t.Fatalf("receive: %v", err)
	}
	doc.Headings[0] = "Changed"
	doc.Outlinks[0] = "https://changed.example/"
	doc.Inlinks[0].Text = "changed"
	doc.Metadata["url_hash"] = "changed"

	got, ok, err := directory.Document(context.Background(), doc.NormalizedURL)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if !ok {
		t.Fatal("document missing")
	}
	if got.CanonicalURL != doc.NormalizedURL {
		t.Fatalf("canonical URL = %q", got.CanonicalURL)
	}
	if got.Headings[0] != "Heading" || got.Outlinks[0] != "https://example.org/a" ||
		got.Inlinks[0].Text != "anchor" || got.Metadata["url_hash"] != "abc" {
		t.Fatalf("document retained caller mutation: %#v", got)
	}
}

func TestReceiveNoopsWithoutDocuments(t *testing.T) {
	_, receiver := openDocuments(t)
	receipt, err := receiver.Receive(context.Background(), nil)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if receipt != (Receipt{}) {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestReceiveUpdatesDocument(t *testing.T) {
	directory, receiver := openDocuments(t)
	first := Document{NormalizedURL: "https://example.org/", Title: "First"}
	second := Document{NormalizedURL: first.NormalizedURL, Title: "Second"}

	if _, err := receiver.Receive(context.Background(), []Document{first}); err != nil {
		t.Fatalf("first receive: %v", err)
	}
	receipt, err := receiver.Receive(context.Background(), []Document{second})
	if err != nil {
		t.Fatalf("second receive: %v", err)
	}
	if receipt.Updated != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}

	got, ok, err := directory.Document(context.Background(), first.NormalizedURL)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if !ok || got.Title != "Second" {
		t.Fatalf("document = %#v found=%v", got, ok)
	}
}

func TestReceiveRejectsDocumentWithoutURL(t *testing.T) {
	_, receiver := openDocuments(t)
	receipt, err := receiver.Receive(context.Background(), []Document{{Title: "No URL"}})
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if receipt.Rejected != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestReceiveReportsBusyAtCapacity(t *testing.T) {
	_, _, receiver := openDocumentsWithVault(t, 1)
	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/a"}},
	); err != nil {
		t.Fatalf("first receive: %v", err)
	}

	receipt, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/b"}},
	)
	if err != nil {
		t.Fatalf("second receive: %v", err)
	}
	if !receipt.Busy {
		t.Fatalf("receipt = %#v, want busy", receipt)
	}
}

func TestReceiveReturnsCapacityCheckError(t *testing.T) {
	v, _, receiver := openDocumentsWithVault(t, 0)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/"}},
	); err == nil {
		t.Fatal("expected receive error")
	}
}

func TestReceiveReturnsContextErrorInsideStore(t *testing.T) {
	_, receiver := openDocuments(t)
	ctx := &errAfterContext{
		Context:   context.Background(),
		remaining: 2,
		err:       context.Canceled,
	}

	if _, err := receiver.Receive(
		ctx,
		[]Document{{NormalizedURL: "https://example.org/"}},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("receive error = %v, want context.Canceled", err)
	}
}

func TestReceiveReturnsPutError(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	sentinel := errors.New("put failed")
	engine.putErrors[bucketName] = sentinel

	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/"}},
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("receive error = %v, want %v", err, sentinel)
	}
}

func TestReceiveReturnsReadError(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/"}},
	); err != nil {
		t.Fatalf("receive: %v", err)
	}
	engine.buckets[bucketName]["https://example.org/"] = []byte("{")

	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/"}},
	); err == nil {
		t.Fatal("expected receive error")
	}
}

func TestReceiveBoundsExtractedText(t *testing.T) {
	directory, receiver := openDocuments(t)
	text := strings.Repeat("x", maxExtractedTextBytes+32)
	doc := Document{NormalizedURL: "https://example.org/", ExtractedText: text}

	if _, err := receiver.Receive(context.Background(), []Document{doc}); err != nil {
		t.Fatalf("receive: %v", err)
	}

	got, ok, err := directory.Document(context.Background(), doc.NormalizedURL)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if !ok {
		t.Fatal("document missing")
	}
	if len(got.ExtractedText) > maxExtractedTextBytes {
		t.Fatalf("extracted text length = %d", len(got.ExtractedText))
	}
}

func TestDocumentReturnsMissing(t *testing.T) {
	directory, _ := openDocuments(t)
	got, ok, err := directory.Document(context.Background(), "https://example.org/missing")
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if ok || got.NormalizedURL != "" || got.Title != "" || got.ExtractedText != "" {
		t.Fatalf("document = %#v found=%v, want missing", got, ok)
	}
}

func TestDocumentReturnsReadError(t *testing.T) {
	v, directory, _ := openDocumentsWithVault(t, 0)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := directory.Document(context.Background(), "https://example.org/"); err == nil {
		t.Fatal("expected document error")
	}
}

func TestDocumentReturnsDecodeError(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/"}},
	); err != nil {
		t.Fatalf("receive: %v", err)
	}
	engine.buckets[bucketName]["https://example.org/"] = []byte("{")

	if _, _, err := directory.Document(context.Background(), "https://example.org/"); err == nil {
		t.Fatal("expected document error")
	}
}

func TestCountReportsStoredDocuments(t *testing.T) {
	directory, receiver := openDocuments(t)
	if _, err := receiver.Receive(context.Background(), []Document{
		{NormalizedURL: "https://example.org/a"},
		{NormalizedURL: "https://example.org/b"},
	}); err != nil {
		t.Fatalf("receive: %v", err)
	}

	count, err := directory.Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestCountReturnsLengthError(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/"}},
	); err != nil {
		t.Fatalf("receive: %v", err)
	}
	engine.buckets[vault.Name("__lengths__")][string(bucketName)] = []byte("bad")

	if _, err := directory.Count(context.Background()); err == nil {
		t.Fatal("expected count error")
	}
}

func TestCountReturnsReadError(t *testing.T) {
	v, directory, _ := openDocumentsWithVault(t, 0)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := directory.Count(context.Background()); err == nil {
		t.Fatal("expected count error")
	}
}

func TestStoredDocumentsStopsWhenVisitorStops(t *testing.T) {
	directory, receiver := openDocuments(t)
	if _, err := receiver.Receive(context.Background(), []Document{
		{NormalizedURL: "https://example.org/a"},
		{NormalizedURL: "https://example.org/b"},
	}); err != nil {
		t.Fatalf("receive: %v", err)
	}

	var visits int
	err := directory.(StoredDocuments).StoredDocuments(
		context.Background(),
		func(Document) (bool, error) {
			visits++
			return false, nil
		},
	)
	if err != nil {
		t.Fatalf("stored documents: %v", err)
	}
	if visits != 1 {
		t.Fatalf("visits = %d, want 1", visits)
	}
}

func TestStoredDocumentsReturnsVisitorError(t *testing.T) {
	directory, receiver := openDocuments(t)
	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/a"}},
	); err != nil {
		t.Fatalf("receive: %v", err)
	}
	sentinel := errors.New("visitor failed")

	err := directory.(StoredDocuments).StoredDocuments(
		context.Background(),
		func(Document) (bool, error) {
			return false, sentinel
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("stored documents error = %v, want %v", err, sentinel)
	}
}

func TestStoredDocumentsReturnsContextErrorInsideScan(t *testing.T) {
	directory, receiver := openDocuments(t)
	if _, err := receiver.Receive(
		context.Background(),
		[]Document{{NormalizedURL: "https://example.org/a"}},
	); err != nil {
		t.Fatalf("receive: %v", err)
	}
	ctx := &errAfterContext{
		Context:   context.Background(),
		remaining: 2,
		err:       context.Canceled,
	}

	err := directory.(StoredDocuments).StoredDocuments(
		ctx,
		func(Document) (bool, error) {
			t.Fatal("visitor should not run after context error")
			return true, nil
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("stored documents error = %v, want context.Canceled", err)
	}
}

func TestStoredDocumentsReturnsReadError(t *testing.T) {
	v, directory, _ := openDocumentsWithVault(t, 0)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	err := directory.(StoredDocuments).StoredDocuments(
		context.Background(),
		func(Document) (bool, error) { return true, nil },
	)
	if err == nil {
		t.Fatal("expected stored documents error")
	}
}

func TestOpenReturnsRegisterError(t *testing.T) {
	if _, _, err := Open(nil); err == nil {
		t.Fatal("expected open error")
	}
}

func TestDocumentCodecRejectsInvalidJSON(t *testing.T) {
	if _, err := (documentCodec{}).Decode([]byte("{")); err == nil {
		t.Fatal("expected decode error")
	}
}
