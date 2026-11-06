package searchindex

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSentenceSnippetAlignsToSentenceBoundaries(t *testing.T) {
	text := strings.Repeat("Leading filler words. ", 10) + "Opening sentence here. " +
		"The needle compass sits in this sentence. " + strings.Repeat("Trailing filler words. ", 20)
	start := strings.Index(text, "needle")
	got := sentenceSnippet(textWitness(text, start, start+len("needle compass")), "fallback")
	if !strings.HasPrefix(
		got,
		"… Leading filler words. Leading filler words. Opening sentence here. The needle compass",
	) ||
		!strings.HasSuffix(got, "Trailing filler words.") {
		t.Fatalf("snippet = %q", got)
	}
}

func TestSentenceSnippetTreatsANewlineAsAParagraphBoundary(t *testing.T) {
	text := strings.Repeat("lead ", 70) + "\nheading without punctuation needle " +
		strings.Repeat("tail ", 70)
	start := strings.Index(text, "needle")
	got := sentenceSnippet(textWitness(text, start, start+len("needle")), "fallback")
	if !strings.HasPrefix(got, "… heading without punctuation needle") {
		t.Fatalf("snippet = %q", got)
	}
}

func TestSentenceSnippetKeepsTheBoundedWindowWithoutASafeBoundary(t *testing.T) {
	for _, separator := range []string{", ", "; ", ".", " "} {
		text := strings.Repeat(
			"word"+separator,
			120,
		) + "needle" + strings.Repeat(
			separator+"word",
			120,
		)
		start := strings.Index(text, "needle")
		witness := textWitness(text, start, start+len("needle"))
		if got := sentenceSnippet(
			witness,
			"fallback",
		); got != boundedSnippet(
			text,
			witness.evidence,
			"fallback",
		) {
			t.Fatalf("separator %q moved the window: %q", separator, got)
		}
	}
}

func TestSentenceSnippetNeverCutsInsideTheWitness(t *testing.T) {
	text := strings.Repeat("lead ", 40) + "Needle. Compass" + strings.Repeat(" tail", 80)
	start := strings.Index(text, "Needle")
	witness := textWitness(text, start, start+len("Needle. Compass"))
	if got := sentenceSnippet(
		witness,
		"fallback",
	); got != boundedSnippet(
		text,
		witness.evidence,
		"fallback",
	) {
		t.Fatalf("witness cut: %q", got)
	}
}

func TestSentenceSnippetKeepsAWideWitnessWhole(t *testing.T) {
	text := strings.Repeat("lead ", 40) + "needle " + strings.Repeat("ж", 290) + " compass" +
		strings.Repeat(" tail", 40)
	start := strings.Index(text, "needle")
	end := strings.Index(text, "compass") + len("compass")
	got := sentenceSnippet(textWitness(text, start, end), "fallback")
	if !strings.Contains(got, text[start:end]) {
		t.Fatalf("wide witness cut: %q", got)
	}
}

func TestSentenceSnippetKeepsMultibyteTextValid(t *testing.T) {
	text := strings.Repeat("слово ", 40) + "Где игла? Здесь игла и компас! " +
		strings.Repeat("слово ", 60)
	start := strings.Index(text, "игла и компас")
	got := sentenceSnippet(textWitness(text, start, start+len("игла и компас")), "fallback")
	if !utf8.ValidString(got) || got != "… Здесь игла и компас!" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestSentenceSnippetReturnsAWholeTextUnchanged(t *testing.T) {
	text := "First sentence. The needle sits here. Last sentence."
	start := strings.Index(text, "needle")
	if got := sentenceSnippet(
		textWitness(text, start, start+len("needle")),
		"fallback",
	); got != text {
		t.Fatalf("snippet = %q", got)
	}
}

func textWitness(text string, start int, end int) snippetWitness {
	return snippetWitness{text: text, evidence: TextQueryEvidence{Start: start, End: end}}
}
