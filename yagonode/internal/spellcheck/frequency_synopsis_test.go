package spellcheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestFrequencySynopsisKeepsBoundedHeavyTerms(t *testing.T) {
	synopsis := NewFrequencySynopsis(2)
	synopsis.ObserveText("alpha alpha beta gamma gamma gamma")
	got := synopsis.Frequencies()
	if len(got) != 2 || got["alpha"] != 2 || got["gamma"] != 3 {
		t.Fatalf("frequencies = %#v", got)
	}
}

func TestFrequencySynopsisBoundsAdversarialCardinality(t *testing.T) {
	const limit = 128
	synopsis := NewFrequencySynopsis(limit)
	for index := range 100_000 {
		synopsis.ObserveText(fmt.Sprintf("term%06d", index))
	}
	if got := len(synopsis.Frequencies()); got != limit {
		t.Fatalf("terms = %d, want %d", got, limit)
	}
}

func TestFrequencySynopsisHandlesZeroLimitAndFrequencyTies(t *testing.T) {
	empty := NewFrequencySynopsis(-1)
	empty.ObserveText("alpha beta")
	if len(empty.Frequencies()) != 0 {
		t.Fatalf("zero-limit frequencies = %#v", empty.Frequencies())
	}

	synopsis := NewFrequencySynopsis(2)
	synopsis.ObserveText("beta alpha beta")
	if got := synopsis.Frequencies(); got["beta"] != 2 || got["alpha"] != 1 {
		t.Fatalf("tied frequencies = %#v", got)
	}
}

func BenchmarkFrequencyCollection(b *testing.B) {
	terms := make([]string, 100_000)
	for index := range terms {
		terms[index] = fmt.Sprintf("term%06d", index)
	}
	corpus := strings.Join(terms, " ")
	b.Run("exact", func(b *testing.B) {
		for range b.N {
			frequency := map[string]int{}
			TermFrequencies(frequency, corpus)
		}
	})
	b.Run("bounded", func(b *testing.B) {
		for range b.N {
			synopsis := NewFrequencySynopsis(8_192)
			synopsis.ObserveText(corpus)
		}
	})
}
