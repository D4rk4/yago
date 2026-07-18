// Package wordforms expands a query word into the inflected surface forms that
// actually occur in the node's index. YaCy word hashes cover exact words with no
// stemming, so a swarm search for a base form misses peer documents indexed under
// an inflected form. Rather than hardcode any language's endings, the expander
// groups the corpus vocabulary by the stem the per-language analyzer produces
// (the same Snowball stemmers the index uses) and returns the observed forms that
// share the query word's stem — so it generalizes to every language with a
// stemmer and only ever emits real, wire-compatible exact-word forms.
package wordforms

import (
	"sort"
	"strings"
)

const (
	// minVariantLen skips short words where a shared stem is too weak a signal.
	minVariantLen   = 4
	MaximumVariants = 6
	// maxFormsPerStem caps how many surface forms a stem retains, keeping the most
	// frequent ones.
	maxFormsPerStem = 8
)

// Expander maps a stem to the surface forms seen for it in the corpus, built from
// the vocabulary with an injected stemmer. The zero value expands nothing, so an
// unbuilt expander is safe to consult.
type Expander struct {
	formsByStem map[string][]string
	stem        func(string) string
}

type countedForm struct {
	form  string
	count int
}

// New builds an expander from a term→frequency vocabulary and a stemmer. Terms
// are grouped by stem; each stem keeps its most frequent surface forms.
func New(vocabulary map[string]int, stem func(string) string) *Expander {
	expander := &Expander{formsByStem: map[string][]string{}, stem: stem}
	if stem == nil {
		return expander
	}
	grouped := map[string][]countedForm{}
	for term, count := range vocabulary {
		term = strings.ToLower(strings.TrimSpace(term))
		if len([]rune(term)) < minVariantLen || count <= 0 {
			continue
		}
		key := stem(term)
		grouped[key] = append(grouped[key], countedForm{form: term, count: count})
	}
	for key, forms := range grouped {
		expander.formsByStem[key] = topForms(forms)
	}

	return expander
}

// Variants returns the query word followed by the other observed surface forms
// sharing its stem, the original always first, bounded and de-duplicated. A short
// word, an empty expander, or a stem with no other forms returns just the word.
func (e *Expander) Variants(word string) []string {
	word = strings.ToLower(strings.TrimSpace(word))
	if e == nil || e.stem == nil || len([]rune(word)) < minVariantLen {
		return normalizeVariants(word, nil)
	}

	return normalizeVariants(word, e.formsByStem[e.stem(word)])
}

// topForms orders a stem's forms by descending frequency (then the form itself
// for determinism) and caps them.
func topForms(forms []countedForm) []string {
	sort.Slice(forms, func(i, j int) bool {
		if forms[i].count != forms[j].count {
			return forms[i].count > forms[j].count
		}

		return forms[i].form < forms[j].form
	})
	out := make([]string, 0, maxFormsPerStem)
	for _, form := range forms {
		out = append(out, form.form)
		if len(out) == maxFormsPerStem {
			break
		}
	}

	return out
}

// normalizeVariants puts the original word first, drops blanks and duplicates,
// and caps the result.
func normalizeVariants(word string, extra []string) []string {
	out := make([]string, 0, MaximumVariants)
	seen := map[string]bool{}
	for _, candidate := range append([]string{word}, extra...) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
		if len(out) == MaximumVariants {
			break
		}
	}

	return out
}
