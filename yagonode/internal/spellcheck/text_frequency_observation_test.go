package spellcheck

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestTextFrequencyObservationPreservesUnicodeAndPunctuation(t *testing.T) {
	var terms []string
	text := "ČETIRI—КРАЇНА_42,東京123;NAÏVE/CAFÉ;can't e\u0301lan ALPHA" +
		string([]byte{0xff}) + "BRAVO"
	termsInText(text, func(term string) {
		terms = append(terms, term)
	})
	want := []string{"četiri", "країна_42", "東京123", "naïve", "café", "alpha", "bravo"}
	if !slices.Equal(terms, want) {
		t.Fatalf("terms = %q, want %q", terms, want)
	}
}

func TestTextFrequencyObservationPreservesLengthLimits(t *testing.T) {
	short := strings.Repeat("界", defaultMinTermLen-1)
	minimum := strings.Repeat("界", defaultMinTermLen)
	maximum := strings.Repeat("界", defaultMaxTermLen)
	long := strings.Repeat("界", defaultMaxTermLen+1)
	var terms []string
	termsInText(strings.Join([]string{short, minimum, maximum, long}, " "), func(term string) {
		terms = append(terms, term)
	})
	want := []string{minimum, maximum}
	if !slices.Equal(terms, want) {
		t.Fatalf("terms = %q, want %q", terms, want)
	}
}

func TestRejectedUppercaseTermDoesNotAllocateCaseFolding(t *testing.T) {
	uppercase := strings.Repeat("A", 1<<12)
	lowercase := strings.Repeat("a", 1<<12)
	synopsis := NewFrequencySynopsis(1)
	uppercaseAllocations := testing.AllocsPerRun(20, func() {
		synopsis.ObserveText(uppercase)
	})
	lowercaseAllocations := testing.AllocsPerRun(20, func() {
		synopsis.ObserveText(lowercase)
	})
	if uppercaseAllocations > lowercaseAllocations {
		t.Fatalf(
			"uppercase rejected-term allocations = %v, lowercase = %v",
			uppercaseAllocations,
			lowercaseAllocations,
		)
	}
}

func TestSharedTextFrequencyObservationMatchesSeparateSynopses(t *testing.T) {
	texts := []string{
		"ALPHA alpha bravo charlie delta echo",
		"BRAVO bravo delta foxtrot golf hotel",
		"ČETIRI—КРАЇНА_42 東京123 NAÏVE CAFÉ",
	}
	separateSpell := NewFrequencySynopsis(3)
	separateForms := NewFrequencySynopsis(5)
	sharedSpell := NewFrequencySynopsis(3)
	sharedForms := NewFrequencySynopsis(5)
	for _, text := range texts {
		separateSpell.ObserveText(text)
		separateForms.ObserveText(text)
		ObserveTextFrequencies(text, nil, sharedSpell, NewFrequencySynopsis(0), sharedForms)
	}
	gotSpell := sharedSpell.Frequencies()
	wantSpell := separateSpell.Frequencies()
	if !reflect.DeepEqual(gotSpell, wantSpell) {
		t.Fatalf("shared spelling frequencies = %#v, want %#v", gotSpell, wantSpell)
	}
	gotForms := sharedForms.Frequencies()
	wantForms := separateForms.Frequencies()
	if !reflect.DeepEqual(gotForms, wantForms) {
		t.Fatalf("shared word-form frequencies = %#v, want %#v", gotForms, wantForms)
	}

	ObserveTextFrequencies("IGNORED", nil, NewFrequencySynopsis(0))
}

func FuzzTextFrequencyObservationMatchesPreviousSemantics(f *testing.F) {
	seeds := []string{
		"",
		"ČETIRI—КРАЇНА_42,東京123;NAÏVE/CAFÉ",
		"ALPHA" + string([]byte{0xff}) + "BRAVO",
		"e\u0301lan élan KELVIN १२३४",
		strings.Repeat("a", defaultMinTermLen-1) + " " +
			strings.Repeat("b", defaultMinTermLen) + " " +
			strings.Repeat("c", defaultMaxTermLen) + " " +
			strings.Repeat("d", defaultMaxTermLen+1),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		var got []string
		termsInText(text, func(term string) {
			got = append(got, term)
		})
		var want []string
		for _, term := range strings.FieldsFunc(strings.ToLower(text), isNotWordRune) {
			if correctableTerm(term) {
				want = append(want, term)
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("terms = %q, want %q", got, want)
		}
	})
}

func BenchmarkTextFrequencyObservation(b *testing.B) {
	text := strings.Repeat("MONTENEGRO ALGORITHM ČETIRI TUTORIAL ", 64)
	b.Run("separate", func(b *testing.B) {
		spell := NewFrequencySynopsis(32)
		forms := NewFrequencySynopsis(64)
		spell.ObserveText(text)
		forms.ObserveText(text)
		b.SetBytes(int64(len(text)))
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			spell.ObserveText(text)
			forms.ObserveText(text)
		}
		b.ReportMetric(2, "text-passes/op")
	})
	b.Run("shared", func(b *testing.B) {
		spell := NewFrequencySynopsis(32)
		forms := NewFrequencySynopsis(64)
		ObserveTextFrequencies(text, spell, forms)
		b.SetBytes(int64(len(text)))
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			ObserveTextFrequencies(text, spell, forms)
		}
		b.ReportMetric(1, "text-passes/op")
	})
}
