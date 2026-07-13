package documentstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type scriptedDocumentEngine struct {
	buckets         map[vault.Name]map[string][]byte
	provisionErrors map[vault.Name]error
	putErrors       map[vault.Name]error
	delErrors       map[vault.Name]error
	replayNext      bool
	commitFirst     bool
}

func newScriptedDocumentEngine() *scriptedDocumentEngine {
	return &scriptedDocumentEngine{
		buckets:         map[vault.Name]map[string][]byte{},
		provisionErrors: map[vault.Name]error{},
		putErrors:       map[vault.Name]error{},
		delErrors:       map[vault.Name]error{},
	}
}

func (e *scriptedDocumentEngine) Provision(name vault.Name) error {
	if err := e.provisionErrors[name]; err != nil {
		return err
	}
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}
	return nil
}

func (e *scriptedDocumentEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if e.replayNext {
		e.replayNext = false
		before := cloneDocumentBuckets(e.buckets)
		if err := fn(scriptedDocumentTxn{engine: e, writable: true}); err != nil {
			return err
		}
		if !e.commitFirst {
			e.buckets = before
		}
	}
	return fn(scriptedDocumentTxn{engine: e, writable: true})
}

func cloneDocumentBuckets(
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
	if err := b.engine.delErrors[b.name]; err != nil {
		return err
	}
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
	if len(receipt.CommittedDocuments) != 1 ||
		receipt.CommittedDocuments[0].Title != doc.Title {
		t.Fatalf("committed documents = %#v", receipt.CommittedDocuments)
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

func TestReceivePublishesOnlyTheCommittedReplayReceipt(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	engine.replayNext = true
	receipt, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: "https://example.org/a"},
		{NormalizedURL: "https://example.org/b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Stored != 2 || receipt.Updated != 0 || len(receipt.CommittedDocuments) != 2 {
		t.Fatalf("receipt = %#v, want one committed attempt", receipt)
	}
}

func TestReceiveDefaultsCanonicalURLAndCopiesValues(t *testing.T) {
	directory, receiver := openDocuments(t)
	doc := Document{
		NormalizedURL: "https://example.org/",
		Headings:      []string{"Heading"},
		Outlinks:      []string{"https://example.org/a"},
		Inlinks:       []AnchorText{{URL: "https://example.org/from", Text: "anchor"}},
		OutboundAnchors: []OutboundAnchor{{
			TargetURL: "https://example.org/target", Text: "target",
		}},
		OutboundAnchorEvidenceKnown: true,
		Images: []ImageMetadata{
			{URL: "https://example.org/image.png", AltText: "image"},
		},
		Metadata: map[string]string{"url_hash": "abc"},
	}

	if _, err := receiver.Receive(context.Background(), []Document{doc}); err != nil {
		t.Fatalf("receive: %v", err)
	}
	doc.Headings[0] = "Changed"
	doc.Outlinks[0] = "https://changed.example/"
	doc.Inlinks[0].Text = "changed"
	doc.OutboundAnchors[0].Text = "changed"
	doc.Images[0].AltText = "changed"
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
		got.Inlinks[0].Text != "anchor" || got.Images[0].AltText != "image" ||
		got.OutboundAnchors[0].Text != "target" || !got.OutboundAnchorEvidenceKnown ||
		got.Metadata["url_hash"] != "abc" {
		t.Fatalf("document retained caller mutation: %#v", got)
	}
}

func TestReceiveNoopsWithoutDocuments(t *testing.T) {
	_, receiver := openDocuments(t)
	receipt, err := receiver.Receive(context.Background(), nil)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if receipt.Busy || receipt.Stored != 0 || receipt.Updated != 0 ||
		receipt.Rejected != 0 || len(receipt.CommittedDocuments) != 0 {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestCanonicalDocumentsMergePersistedEvidenceWithoutWriting(t *testing.T) {
	directory, receiver := openDocuments(t)
	url := "https://target.example/"
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		Title:         "stored",
		ContentHash:   "same",
	}}); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	if _, err := anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: "https://source.example/",
			Anchors:   []OutboundAnchor{{TargetURL: url, Text: "trusted"}},
		}},
	); err != nil {
		t.Fatalf("seed inbound anchor: %v", err)
	}

	canonical := receiver.(CanonicalDocumentDirectory)
	docs, err := canonical.CanonicalDocuments(t.Context(), []Document{
		{NormalizedURL: url, Title: "incoming", ContentHash: "same"},
		{Title: "rejected"},
	})
	if err != nil || len(docs) != 1 || docs[0].Title != "incoming" ||
		len(docs[0].Inlinks) != 1 || docs[0].Inlinks[0].Text != "trusted" {
		t.Fatalf("canonical documents = %#v, %v", docs, err)
	}
	stored, found, err := directory.Document(t.Context(), url)
	if err != nil || !found || stored.Title != "stored" {
		t.Fatalf("stored document = %#v, %v, %v", stored, found, err)
	}
}

func TestCanonicalDocumentsSurfaceContextAndVaultFailures(t *testing.T) {
	v, _, receiver := openDocumentsWithVault(t, 0)
	canonical := receiver.(CanonicalDocumentDirectory)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := canonical.CanonicalDocuments(cancelled, []Document{{
		NormalizedURL: "https://example.org/",
	}}); err == nil {
		t.Fatal("cancelled canonicalization succeeded")
	}
	delayed := &errAfterContext{
		Context:   context.Background(),
		remaining: 2,
		err:       context.Canceled,
	}
	if _, err := canonical.CanonicalDocuments(delayed, []Document{{
		NormalizedURL: "https://example.org/",
	}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("delayed cancellation = %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}
	if _, err := canonical.CanonicalDocuments(t.Context(), []Document{{
		NormalizedURL: "https://example.org/",
	}}); err == nil {
		t.Fatal("closed vault canonicalization succeeded")
	}
}

func TestCanonicalDocumentsSurfaceStoredDecodeFailure(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/"
	engine.buckets[bucketName][url] = []byte("invalid")
	canonical := receiver.(CanonicalDocumentDirectory)
	if _, err := canonical.CanonicalDocuments(t.Context(), []Document{{
		NormalizedURL: url,
	}}); err == nil {
		t.Fatal("invalid stored document canonicalization succeeded")
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

func TestDocumentPresenceChecksRawEntriesWithoutDecoding(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	presence := directory.(DocumentPresence)
	url := "https://example.org/raw"
	engine.buckets[bucketName][url] = []byte("{")

	found, err := presence.DocumentExists(t.Context(), url)
	if err != nil || !found {
		t.Fatalf("existing presence = %t, %v", found, err)
	}
	found, err = presence.DocumentExists(t.Context(), "https://example.org/missing")
	if err != nil || found {
		t.Fatalf("missing presence = %t, %v", found, err)
	}
}

func TestDocumentPresenceReturnsVaultFailure(t *testing.T) {
	v, directory, _ := openDocumentsWithVault(t, 0)
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := directory.(DocumentPresence).DocumentExists(
		t.Context(),
		"https://example.org/",
	); err == nil {
		t.Fatal("closed vault presence succeeded")
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

func TestDeleteRemovesDocument(t *testing.T) {
	directory, receiver := openDocuments(t)
	doc := Document{
		CanonicalURL:  "https://example.org/",
		NormalizedURL: "https://example.org/",
		Title:         "Example",
	}
	if _, err := receiver.Receive(context.Background(), []Document{doc}); err != nil {
		t.Fatalf("receive: %v", err)
	}

	removed, err := directory.(DocumentEvictor).Delete(context.Background(), doc.NormalizedURL)
	if err != nil || !removed {
		t.Fatalf("delete = %v, %v; want removed", removed, err)
	}
	if _, ok, _ := directory.Document(context.Background(), doc.NormalizedURL); ok {
		t.Fatal("document should be gone after delete")
	}
}

func TestDeleteAbsentDocumentIsNoOp(t *testing.T) {
	directory, _ := openDocuments(t)

	removed, err := directory.(DocumentEvictor).Delete(
		context.Background(),
		"https://absent.example/",
	)
	if err != nil || removed {
		t.Fatalf("delete absent = %v, %v; want (false, nil)", removed, err)
	}
}

func TestDeleteReturnsUpdateError(t *testing.T) {
	directory, _, _ := openScriptedDocuments(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := directory.(DocumentEvictor).Delete(ctx, "https://x.example/"); err == nil {
		t.Fatal("delete should surface an update error")
	}
}

func TestDeleteReturnsCollectionError(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	doc := Document{CanonicalURL: "https://x.example/", NormalizedURL: "https://x.example/"}
	if _, err := receiver.Receive(context.Background(), []Document{doc}); err != nil {
		t.Fatalf("receive: %v", err)
	}
	engine.delErrors[bucketName] = errors.New("delete boom")

	if _, err := directory.(DocumentEvictor).Delete(
		context.Background(),
		doc.NormalizedURL,
	); err == nil {
		t.Fatal("delete should surface a collection error")
	}
}
