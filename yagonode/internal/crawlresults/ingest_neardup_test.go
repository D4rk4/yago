package crawlresults_test

import (
	"context"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/neardup"
)

const nearDupPageText = "Обсуждение настройки маршрутизатора платформа гипервизор " +
	"версия подключение интерфейс конфигурация туннель политика маршрут перезагрузка " +
	"обновление прошивка консоль команда вывод журнал ошибка отладка мониторинг"

func deliverPage(
	t *testing.T,
	stream *fakeStream,
	url string,
	text string,
) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: url,
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: url,
				ExtractedText: text,
			},
		},
		Ack: func(context.Context) error { wg.Done(); return nil },
		Nak: func(context.Context) error { wg.Done(); return nil },
	}
	wg.Wait()
}

// TestIngestCollapsesNearDuplicateDocuments is the CRAWL-10 acceptance: a page
// whose text near-duplicates an already stored page keeps its URL metadata and
// postings but is not stored or indexed as another copy.
func TestIngestCollapsesNearDuplicateDocuments(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	index := &recordingSearchIndex{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	consumer := crawlresults.NewIngestConsumerWithIndex(stream, documents, index, urls, postings)
	consumer.CollapseNearDuplicates(neardup.NewWindow(16))
	consumer.CollapseNearDuplicates(nil)
	observer := &recordingObserver{}
	consumer.Observe(observer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	deliverPage(t, stream, "https://example.org/page", nearDupPageText)
	if documents.calls != 1 || index.calls != 1 {
		t.Fatalf("first page not stored: docs=%d index=%d", documents.calls, index.calls)
	}

	deliverPage(
		t,
		stream,
		"https://example.org/page?view=print",
		nearDupPageText+" футер",
	)
	if documents.calls != 1 || index.calls != 1 {
		t.Fatalf("near-duplicate stored anyway: docs=%d index=%d", documents.calls, index.calls)
	}
	if urls.calls != 2 || postings.calls != 2 {
		t.Fatalf(
			"duplicate lost metadata/postings: urls=%d postings=%d",
			urls.calls,
			postings.calls,
		)
	}
	if observer.duplicates != 1 || observer.absorbed != 2 {
		t.Fatalf("observer = %+v", observer)
	}

	deliverPage(t, stream, "https://example.org/page", nearDupPageText+" обновление")
	if documents.calls != 2 {
		t.Fatalf("refetch of the original collapsed: docs=%d", documents.calls)
	}
}

func TestIngestNearDuplicateKeyFallsBackToCanonicalURL(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	consumer := crawlresults.NewIngestConsumer(stream, documents, urls, postings)
	consumer.CollapseNearDuplicates(neardup.NewWindow(16))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://example.org/canonical-only",
			Document: yagocrawlcontract.DocumentIngest{
				CanonicalURL:  "https://example.org/canonical-only",
				ExtractedText: nearDupPageText,
			},
		},
		Ack: func(context.Context) error { wg.Done(); return nil },
		Nak: func(context.Context) error { wg.Done(); return nil },
	}
	wg.Wait()
	if documents.calls != 1 {
		t.Fatalf("canonical-keyed page not stored: %d", documents.calls)
	}
}

func TestNoopObserverDuplicateIsSilent(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	consumer := crawlresults.NewIngestConsumer(stream, documents, urls, postings)
	consumer.CollapseNearDuplicates(neardup.NewWindow(16))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	deliverPage(t, stream, "https://example.org/a", nearDupPageText)
	deliverPage(t, stream, "https://example.org/b", nearDupPageText)
	if documents.calls != 1 {
		t.Fatalf("noop-observer duplicate stored: %d", documents.calls)
	}
}
