package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// TestSearchMorphologyAcrossLanguages is the headline behaviour: a base-form
// query recalls inflected documents in its own language through the routed
// stemmer, without dragging in an unrelated same-script document.
func TestSearchMorphologyAcrossLanguages(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://ru.wikipedia.org/mn",
				Title:         "Черногория",
				ExtractedText: "Черногория — государство на Балканском полуострове, столица Подгорица.",
			},
			{
				NormalizedURL: "https://ru.wikipedia.org/trip",
				Title:         "Поездка",
				ExtractedText: "Мы отдыхали в черногории прошлым летом и всем советуем черногорию.",
			},
			{
				NormalizedURL: "https://anticisco.ru/blog",
				Title:         "antiCisco",
				ExtractedText: "Через современный монитор многие города которые территория " +
					"маршрутизатор настройка openvpn туннель много строгого горы",
			},
			{
				NormalizedURL: "https://de.wikipedia.org/mn",
				Title:         "Montenegro",
				ExtractedText: "Montenegro ist ein Staat auf der Balkanhalbinsel mit hohen Bergen.",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	// Russian base form recalls both the base-form and the two inflected pages,
	// and never the unrelated Cisco blog.
	russian, err := index.Search(t.Context(), SearchRequest{Query: "черногория", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if russian.Total != 2 {
		t.Fatalf("russian morphology total = %d, want 2 (base + inflected)", russian.Total)
	}
	for _, result := range russian.Results {
		if result.URL == "https://anticisco.ru/blog" {
			t.Fatal("unrelated same-script document leaked into results")
		}
	}

	// German stems its own text (Bergen -> Berg).
	german, err := index.Search(t.Context(), SearchRequest{Query: "berg", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if german.Total != 1 || german.Results[0].URL != "https://de.wikipedia.org/mn" {
		t.Fatalf("german morphology = %#v", german)
	}
}
