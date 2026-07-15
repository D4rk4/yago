package searchindex

import (
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

var storedWordFormBenchmarkValue float64

func BenchmarkStoredWordFormProximityComparison(b *testing.B) {
	statement := "observatory telemetry processor transferring operator complete sensor archive remote storage"
	document := documentstore.Document{
		ExtractedText: strings.Repeat("background material without query evidence ", 128) +
			statement + strings.Repeat(" unrelated appendix material", 128),
	}
	terms := strings.Fields(statement)
	b.Run("exact-control", func(b *testing.B) {
		request := SearchRequest{Terms: terms, Phrases: []string{"scan"}}
		b.ReportAllocs()
		for range b.N {
			evidence, err := storedDocumentLocations(b.Context(), document, request, "en")
			if err != nil {
				b.Fatal(err)
			}
			hit := &search.DocumentMatch{Locations: evidence.exactLocations}
			storedWordFormBenchmarkValue = hitUnorderedProximity(
				hit,
				evidence.rawRequirements,
			) + hitOrderedProximity(hit, evidence.rawRequirements)
		}
	})
	b.Run("word-forms", func(b *testing.B) {
		request := SearchRequest{Terms: terms, IncludePositions: true}
		b.ReportAllocs()
		for range b.N {
			evidence, err := storedDocumentLocations(b.Context(), document, request, "en")
			if err != nil {
				b.Fatal(err)
			}
			storedWordFormBenchmarkValue = evidence.proximity + evidence.orderedProximity
		}
	})
}

func BenchmarkStoredWordFormProximityInterleaved(b *testing.B) {
	statement := "observatory telemetry processor transferring operator complete sensor archive remote storage"
	document := documentstore.Document{
		ExtractedText: strings.Repeat("background material without query evidence ", 128) +
			statement + strings.Repeat(" unrelated appendix material", 128),
	}
	terms := strings.Fields(statement)
	controlRequest := SearchRequest{Terms: terms, Phrases: []string{"scan"}}
	wordFormRequest := SearchRequest{Terms: terms, IncludePositions: true}
	controlElapsed := time.Duration(0)
	wordFormElapsed := time.Duration(0)
	control := func() {
		evidence, err := storedDocumentLocations(b.Context(), document, controlRequest, "en")
		if err != nil {
			b.Fatal(err)
		}
		hit := &search.DocumentMatch{Locations: evidence.exactLocations}
		storedWordFormBenchmarkValue = hitUnorderedProximity(
			hit,
			evidence.rawRequirements,
		) + hitOrderedProximity(hit, evidence.rawRequirements)
	}
	wordForms := func() {
		evidence, err := storedDocumentLocations(b.Context(), document, wordFormRequest, "en")
		if err != nil {
			b.Fatal(err)
		}
		storedWordFormBenchmarkValue = evidence.proximity + evidence.orderedProximity
	}
	b.ResetTimer()
	for index := range b.N {
		if index%2 == 0 {
			started := time.Now()
			control()
			controlElapsed += time.Since(started)
			started = time.Now()
			wordForms()
			wordFormElapsed += time.Since(started)

			continue
		}
		started := time.Now()
		wordForms()
		wordFormElapsed += time.Since(started)
		started = time.Now()
		control()
		controlElapsed += time.Since(started)
	}
	b.ReportMetric(float64(controlElapsed.Nanoseconds())/float64(b.N), "control-ns/op")
	b.ReportMetric(float64(wordFormElapsed.Nanoseconds())/float64(b.N), "word-form-ns/op")
}
