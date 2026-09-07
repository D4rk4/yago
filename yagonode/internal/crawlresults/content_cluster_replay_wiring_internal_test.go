package crawlresults

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestContentClusterIndexFailureReplaysEveryMemberAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	directory, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	clusters, err := contentcluster.Open(storage, contentcluster.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	first := clusteredIngestBatch("https://first.example/", false)
	second := clusteredIngestBatch("https://second.example/", true)
	consumer := &IngestConsumer{
		documents: receiver,
		clusters:  clusters,
		index:     &anchorIndexScript{},
		observer:  noopIngestObserver{},
	}
	if consumer.storeDocument(t.Context(), replayDelivery(first, nil), first) {
		t.Fatal("seed document was deferred")
	}
	interrupted := &anchorIndexScript{err: errors.New("index interrupted")}
	consumer.index = interrupted
	naked := false
	if !consumer.storeDocument(t.Context(), replayDelivery(second, &naked), second) || !naked {
		t.Fatal("interrupted cluster projection was not redelivered")
	}
	if _, found, err := directory.Document(t.Context(), second.SourceURL); err != nil || !found {
		t.Fatalf("stored interrupted document = %t, %v", found, err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, receiver, err = documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	clusters, err = contentcluster.Open(storage, contentcluster.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	replayed := &anchorIndexScript{}
	consumer = &IngestConsumer{
		documents: receiver,
		clusters:  clusters,
		index:     replayed,
		observer:  noopIngestObserver{},
	}
	if consumer.storeDocument(t.Context(), replayDelivery(second, nil), second) {
		t.Fatal("replayed cluster projection was deferred")
	}
	assertIndexedDocumentURLs(t, replayed.docs, first.SourceURL, second.SourceURL)
	if consumer.storeDocument(t.Context(), replayDelivery(second, nil), second) {
		t.Fatal("finalized cluster recrawl was deferred")
	}
	if len(replayed.docs) != 3 ||
		replayed.docs[2].NormalizedURL != second.SourceURL {
		t.Fatalf("finalized cluster recrawl indexed %#v", replayed.docs)
	}
}

func clusteredIngestBatch(url string, canonical bool) yagocrawlcontract.IngestBatch {
	document := yagocrawlcontract.DocumentIngest{
		NormalizedURL: url,
		ExtractedText: "alpha beta gamma delta",
		ContentHash:   "same",
	}
	if canonical {
		document.CanonicalURL = url
	}

	return yagocrawlcontract.IngestBatch{SourceURL: url, Document: document}
}

func replayDelivery(
	batch yagocrawlcontract.IngestBatch,
	naked *bool,
) IngestDelivery {
	return IngestDelivery{
		Batch: batch,
		Ack:   func(context.Context) error { return nil },
		Nak: func(context.Context) error {
			if naked != nil {
				*naked = true
			}

			return nil
		},
	}
}

func assertIndexedDocumentURLs(
	t *testing.T,
	documents []documentstore.Document,
	want ...string,
) {
	t.Helper()
	got := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		got[document.NormalizedURL] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("indexed document urls = %#v", got)
	}
	for _, url := range want {
		if _, found := got[url]; !found {
			t.Fatalf("indexed document urls = %#v, missing %q", got, url)
		}
	}
}
