package spellcheck

import (
	"fmt"
	"reflect"
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

func TestFrequencySynopsisOwnsInsertedAndReplacementTerms(t *testing.T) {
	synopsis := NewFrequencySynopsis(1)
	assertOwned := func(text, want string) {
		synopsis.ObserveText(text)
		entry := synopsis.queue[0]
		if entry.term != want {
			t.Fatalf("retained term = %q, want %q", entry.term, want)
		}
		textStart := uintptr(reflect.ValueOf(text).UnsafePointer())
		textEnd := textStart + uintptr(len(text))
		termStart := uintptr(reflect.ValueOf(entry.term).UnsafePointer())
		if termStart >= textStart && termStart < textEnd {
			t.Fatalf("retained term aliases %d-byte input", len(text))
		}
		for term := range synopsis.entries {
			keyStart := uintptr(reflect.ValueOf(term).UnsafePointer())
			if keyStart >= textStart && keyStart < textEnd {
				t.Fatalf("retained map key aliases %d-byte input", len(text))
			}
		}
	}

	assertOwned(strings.Repeat(" ", 1<<19)+"alpha"+strings.Repeat(" ", 1<<19), "alpha")
	assertOwned(strings.Repeat(" ", 1<<19)+"bravo"+strings.Repeat(" ", 1<<19), "bravo")
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
