package spellcheck

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestCorrectorOwnsNormalizedVocabulary(t *testing.T) {
	backing := strings.Repeat("x", 1<<20)
	term := backing[100:106]
	corrector := New(map[string]int{term: 10})
	if len(corrector.vocabulary) != 1 || corrector.vocabulary[0] != term {
		t.Fatalf("vocabulary = %#v", corrector.vocabulary)
	}
	backingStart := uintptr(reflect.ValueOf(backing).UnsafePointer())
	backingEnd := backingStart + uintptr(len(backing))
	termStart := uintptr(reflect.ValueOf(corrector.vocabulary[0]).UnsafePointer())
	if termStart >= backingStart && termStart < backingEnd {
		t.Fatal("corrector vocabulary retained the source backing string")
	}
}

func TestCorrectorAppliesDeterministicFrequencyFirstLimits(t *testing.T) {
	frequency := map[string]int{
		"tutorial": 9,
		"GOLANG":   4,
		"golang":   6,
		"document": 8,
		"language": 7,
	}
	limits := correctorLimits{
		vocabularyTerms:  3,
		deleteReferences: maximumDeleteReferences,
		deleteBytes:      maximumDeleteIndexBytes,
	}
	first := newCorrector(frequency, limits)
	second := newCorrector(frequency, limits)
	if want := []string{
		"golang",
		"tutorial",
		"document",
	}; !reflect.DeepEqual(
		first.vocabulary,
		want,
	) {
		t.Fatalf("vocabulary = %#v, want %#v", first.vocabulary, want)
	}
	if !reflect.DeepEqual(first.frequency, second.frequency) ||
		!reflect.DeepEqual(first.vocabulary, second.vocabulary) ||
		!reflect.DeepEqual(first.deleteIndex, second.deleteIndex) {
		t.Fatal("corrector construction is not deterministic")
	}
	if got, ok := first.Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("bounded suggestion = %q/%v", got, ok)
	}
}

func TestCorrectorDeleteIndexBudgetsAreHardBoundaries(t *testing.T) {
	frequency := map[string]int{"golang": 10, "tutorial": 9}
	variants := sortedDeleteVariants("golang", defaultMaxEditDistance)
	references, retainedBytes := deleteIndexAddition(map[string][]int{}, variants)
	justEnough := newCorrector(frequency, correctorLimits{
		vocabularyTerms:  2,
		deleteReferences: maximumDeleteReferences,
		deleteBytes:      retainedBytes,
	})
	if len(justEnough.frequency) != 1 || justEnough.deleteBytes != retainedBytes {
		t.Fatalf(
			"exact byte budget retained %#v at %d bytes",
			justEnough.frequency,
			justEnough.deleteBytes,
		)
	}
	shortBytes := newCorrector(frequency, correctorLimits{
		vocabularyTerms:  2,
		deleteReferences: maximumDeleteReferences,
		deleteBytes:      retainedBytes - 1,
	})
	shortReferences := newCorrector(frequency, correctorLimits{
		vocabularyTerms:  2,
		deleteReferences: references - 1,
		deleteBytes:      maximumDeleteIndexBytes,
	})
	negative := newCorrector(frequency, correctorLimits{
		vocabularyTerms: -1, deleteReferences: -1, deleteBytes: -1,
	})
	if len(shortBytes.frequency) != 0 || len(shortReferences.frequency) != 0 ||
		len(negative.frequency) != 0 {
		t.Fatalf("under-budget correctors retained %d/%d/%d terms",
			len(shortBytes.frequency), len(shortReferences.frequency), len(negative.frequency))
	}
}

func TestCorrectorProductionBudgetsRetainFunctionalVocabulary(t *testing.T) {
	frequency := make(map[string]int, maximumVocabularyTerms+808)
	frequency["golang"] = 20_000
	for index := range maximumVocabularyTerms + 808 {
		frequency[fmt.Sprintf("term%05d", index)] = 10_000 - index
	}
	corrector := New(frequency)
	if len(corrector.frequency) > maximumVocabularyTerms ||
		corrector.deleteReferences > maximumDeleteReferences ||
		corrector.deleteBytes > maximumDeleteIndexBytes {
		t.Fatalf("retention = %d terms, %d references, %d bytes",
			len(corrector.frequency), corrector.deleteReferences, corrector.deleteBytes)
	}
	if got, ok := corrector.Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("production-budget suggestion = %q/%v", got, ok)
	}
}

func TestCorrectorHandlesMultilingualMorphologyVocabulary(t *testing.T) {
	corrector := New(map[string]int{
		"морфология":      50,
		"морфологический": 40,
		"морфологии":      30,
	})
	if got, ok := corrector.Suggest("морфолгия"); !ok || got != "морфология" {
		t.Fatalf("multilingual suggestion = %q/%v", got, ok)
	}
	if got := corrector.CorrectQuery(
		[]string{"морфолгия", "морфологии"},
	); got != "морфология морфологии" {
		t.Fatalf("multilingual query = %q", got)
	}
}
