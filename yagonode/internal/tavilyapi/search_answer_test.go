package tavilyapi

import (
	"strings"
	"testing"
)

func answerResults() []SearchResult {
	return []SearchResult{
		{Content: "Go is a statically typed language designed at Google. It compiles fast."},
		{Content: "Short frag. The Go toolchain ships a race detector for concurrent code!"},
		{
			Content: "Go is a statically typed language designed at Google. Unrelated gardening advice grows here.",
		},
	}
}

func TestExtractiveAnswerStitchesMatchingSentences(t *testing.T) {
	answer := extractiveAnswer("basic", "go language toolchain", answerResults())
	for _, want := range []string{
		"Go is a statically typed language designed at Google.",
		"The Go toolchain ships a race detector for concurrent code!",
	} {
		if !strings.Contains(answer, want) {
			t.Fatalf("answer missing %q: %q", want, answer)
		}
	}
	if strings.Count(answer, "statically typed") != 1 {
		t.Fatalf("duplicate sentences must dedupe: %q", answer)
	}
	if strings.Contains(answer, "gardening") {
		t.Fatalf("non-matching sentence included: %q", answer)
	}
}

func TestExtractiveAnswerBoundsAndModes(t *testing.T) {
	long := make([]SearchResult, 0, 6)
	sentence := "The quick brown fox jumps over the lazy dog near the river bank today."
	for i := 0; i < 6; i++ {
		long = append(long, SearchResult{Content: strings.Repeat(sentence+" ", 6)})
	}
	basic := extractiveAnswer("basic", "fox", long)
	if len([]rune(basic)) > answerBasicRuneCap {
		t.Fatalf("basic answer over cap: %d", len([]rune(basic)))
	}
	advanced := extractiveAnswer("advanced", "fox", long)
	if len([]rune(advanced)) > answerAdvancedRuneCap {
		t.Fatalf("advanced answer over cap: %d", len([]rune(advanced)))
	}

	if got := extractiveAnswer("basic", "anything", nil); got != "" {
		t.Fatalf("empty results answer = %q", got)
	}
	// Operator-only queries fall back to accepting every sentence.
	if got := extractiveAnswer("basic", "site:example.org -bad", answerResults()); got == "" {
		t.Fatal("operator-only query must still answer")
	}
	if got := clampRunes("abcdef", 4); got != "abcd" {
		t.Fatalf("clamp = %q", got)
	}

	// More than answerTopResults results stop the outer walk; sentences
	// without terminators still count when informative.
	many := make([]SearchResult, 0, answerTopResults+2)
	for i := 0; i < answerTopResults+2; i++ {
		many = append(many, SearchResult{
			Content: "Terminatorless informative sentence about foxes variant " +
				strings.Repeat("x", i+1),
		})
	}
	if got := extractiveAnswer("basic", "foxes", many); got == "" {
		t.Fatal("terminatorless sentences must answer")
	}

	// Distinct multi-sentence snippets exhaust the sentence budget mid-snippet.
	multi := []SearchResult{
		{Content: "Foxes hunt at night quietly. Foxes rest in dens daily. " +
			"Foxes roam wide territories often. Foxes mark trails carefully. " +
			"Foxes avoid humans mostly."},
	}
	got := extractiveAnswer("basic", "foxes", multi)
	if strings.Count(got, "Foxes") != answerMaxSentences {
		t.Fatalf("sentence budget = %d sentences: %q", strings.Count(got, "Foxes"), got)
	}

	// A sentence that would overflow the cap is skipped while shorter ones fit.
	overflow := []SearchResult{
		{Content: "Foxes lead compact lives in small dens. " +
			"Foxes " + strings.Repeat("stretch this sentence far beyond the basic cap ", 10) + "end. " +
			"Foxes nap briefly at noon."},
	}
	got = extractiveAnswer("basic", "foxes", overflow)
	if !strings.Contains(got, "compact lives") || strings.Contains(got, "stretch this sentence") {
		t.Fatalf("cap skip = %q", got)
	}

	// Mid-sentence caps skip further sentences within one snippet.
	packed := []SearchResult{{Content: strings.Repeat(sentence+" ", 20)}}
	if got := extractiveAnswer("basic", "fox", packed); len([]rune(got)) > answerBasicRuneCap {
		t.Fatalf("packed answer over cap: %d", len([]rune(got)))
	}
}

func TestResponseAnswerUsesResults(t *testing.T) {
	req := SearchRequest{Query: "go language", IncludeAnswer: "basic"}
	answer := responseAnswer(req, answerResults())
	if answer == nil || !strings.Contains(*answer, "statically typed") {
		t.Fatalf("answer = %v", answer)
	}
}
