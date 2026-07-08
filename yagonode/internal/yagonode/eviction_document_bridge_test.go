package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

// TestDocumentEvictionURLBridge pins the data-shape assumption ADR-0036 B rests
// on, against the real document and url-metadata stores rather than fakes: a
// document is keyed by its normalized URL, while a purge starts from the URL
// hash and recovers the URL from the metadata row (whose "url" is the
// wire-encoded same string). If the row's decoded URL ever stopped matching the
// document key byte-for-byte, the eviction path would silently orphan documents;
// this test fails loudly instead. The eviction package's fake-based tests cover
// the purge orchestration on top of this bridge.
func TestDocumentEvictionURLBridge(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	ctx := context.Background()

	docDir, docRcv, err := documentstore.Open(v)
	if err != nil {
		t.Fatalf("open documents: %v", err)
	}
	urlDir, _, urlRcv, err := urlmeta.Open(v)
	if err != nil {
		t.Fatalf("open url metadata: %v", err)
	}

	const url = "https://example.org/gone"
	hash, err := yagomodel.HashURL(url)
	if err != nil {
		t.Fatalf("hash url: %v", err)
	}

	// Seed the document (keyed by URL) and its metadata row (keyed by hash, URL
	// wire-encoded) exactly as the crawl ingest does.
	if _, err := docRcv.Receive(ctx, []documentstore.Document{{NormalizedURL: url}}); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	row := yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash: hash.String(),
		yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm(url),
	}}
	if _, err := urlRcv.Receive(ctx, []yagomodel.URIMetadataRow{row}); err != nil {
		t.Fatalf("seed url metadata: %v", err)
	}

	// The bridge: recover the row by hash, decode its "url", and confirm it is
	// the exact document key.
	rows, err := urlDir.RowsByHash(ctx, []yagomodel.Hash{hash.Hash()})
	if err != nil || len(rows) != 1 {
		t.Fatalf("resolve row by hash: rows=%d err=%v", len(rows), err)
	}
	decoded, err := yagomodel.DecodeWireForm(ctx, rows[0].Properties[yagomodel.URLMetaURL])
	if err != nil {
		t.Fatalf("decode url wire form: %v", err)
	}
	if decoded != url {
		t.Fatalf("decoded url %q != document key %q", decoded, url)
	}

	evictor, ok := docDir.(documentstore.DocumentEvictor)
	if !ok {
		t.Fatal("document directory is not a DocumentEvictor")
	}
	removed, err := evictor.Delete(ctx, decoded)
	if err != nil {
		t.Fatalf("delete document by decoded url: %v", err)
	}
	if !removed {
		t.Fatal("the decoded url did not key the seeded document")
	}
	if _, found, _ := docDir.Document(ctx, url); found {
		t.Fatal("document survived deletion by its decoded url")
	}
}
