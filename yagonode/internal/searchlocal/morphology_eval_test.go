package searchlocal_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
)

type storedCorpus struct {
	documents []documentstore.Document
}

func (c storedCorpus) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	for _, doc := range c.documents {
		cont, err := visit(doc)
		if err != nil || !cont {
			return err
		}
	}

	return nil
}

// TestLocalRankingMeetsMorphologyBaseline gates the local ranking against
// regression: base-form queries in several languages must recall their inflected
// documents ahead of same-script noise, keeping mean NDCG@10 at a perfect score
// on this labelled multilingual corpus. A ranking change that reintroduces the
// morphology flood or drops per-language stemming fails here.
func TestLocalRankingMeetsMorphologyBaseline(t *testing.T) {
	corpus := storedCorpus{documents: []documentstore.Document{
		{
			NormalizedURL: "https://ru.example/mn",
			Title:         "Черногория",
			ExtractedText: "Черногория — государство на Балканском полуострове, столица Подгорица.",
		},
		{
			NormalizedURL: "https://ru.example/trip",
			Title:         "Отпуск в Черногории",
			ExtractedText: "Прошлым летом мы отдыхали в черногории и всем советуем черногорию.",
		},
		{
			NormalizedURL: "https://noise.example/cisco",
			Title:         "Сетевой блог",
			ExtractedText: "Современный маршрутизатор настройка openvpn туннель много горы строгого режима.",
		},
		{
			NormalizedURL: "https://de.example/mn",
			Title:         "Montenegro",
			ExtractedText: "Montenegro ist ein Staat auf der Balkanhalbinsel mit hohen Bergen.",
		},
		{
			NormalizedURL: "https://en.example/run",
			Title:         "Marathon training",
			ExtractedText: "She keeps running marathons every spring and runs at dawn.",
		},
	}}

	index, err := searchindex.NewBleveMemoryIndex(t.Context(), corpus)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	searcher := searchlocal.NewSearcher(index)

	judgments := []searcheval.Judgment{
		{Query: "черногория", Relevant: map[string]int{
			"https://ru.example/mn":   2,
			"https://ru.example/trip": 1,
		}},
		{Query: "berge", Relevant: map[string]int{"https://de.example/mn": 1}},
		{Query: "running", Relevant: map[string]int{"https://en.example/run": 1}},
	}

	report, err := searcheval.Evaluate(context.Background(), searcher, judgments, 10)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	const baseline = 1.0
	if report.Mean < baseline {
		t.Fatalf("mean NDCG@%d = %.4f, want >= %.2f (per-query %v)",
			report.K, report.Mean, baseline, report.PerQuery)
	}
}
