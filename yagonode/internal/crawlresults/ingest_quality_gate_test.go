package crawlresults_test

import (
	"context"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

const qualityPageText = "Обсуждение настройки маршрутизатора платформа гипервизор " +
	"версия подключение интерфейс конфигурация туннель политика маршрут перезагрузка " +
	"обновление прошивка консоль команда вывод журнал ошибка отладка мониторинг"

func deliverQualityPage(
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

func TestIngestGateRejectsLowQualityBatchWhole(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	consumer := crawlresults.NewIngestConsumer(stream, documents, urls, postings)
	consumer.GateQuality(func(text string) string {
		if len(text) < 100 {
			return "too-few-words"
		}

		return ""
	})
	consumer.GateQuality(nil)
	observer := &recordingObserver{}
	consumer.Observe(observer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	deliverQualityPage(t, stream, "https://spam.example/doorway", "мало слов")
	if documents.calls != 0 || urls.calls != 0 || postings.calls != 0 {
		t.Fatalf(
			"low-quality batch leaked: docs=%d urls=%d postings=%d",
			documents.calls,
			urls.calls,
			postings.calls,
		)
	}
	if observer.lowQuality != 1 {
		t.Fatalf("lowQuality observations = %d", observer.lowQuality)
	}

	deliverQualityPage(
		t,
		stream,
		"https://ok.example/page",
		qualityPageText+" "+qualityPageText+" достаточно длинный содержательный текст страницы",
	)
	if documents.calls != 1 || urls.calls != 1 || postings.calls != 1 {
		t.Fatalf(
			"passing batch not absorbed: docs=%d urls=%d postings=%d",
			documents.calls,
			urls.calls,
			postings.calls,
		)
	}
}

func TestRejectLowQualityLogsAckFailure(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	urls := &recordingURLReceiver{}
	postings := &recordingPostingReceiver{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	consumer := crawlresults.NewIngestConsumer(stream, documents, urls, postings)
	consumer.GateQuality(func(string) string { return "too-few-words" })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://spam.example/doorway",
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: "https://spam.example/doorway",
				ExtractedText: "мало",
			},
		},
		Ack: func(context.Context) error { wg.Done(); return context.Canceled },
		Nak: func(context.Context) error { wg.Done(); return nil },
	}
	wg.Wait()
	if documents.calls != 0 {
		t.Fatalf("batch stored despite gate: %d", documents.calls)
	}
}
