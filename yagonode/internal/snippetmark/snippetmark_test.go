package snippetmark

import (
	"html/template"
	"testing"
)

func TestHighlightMarksTermsAndEscapesHTML(t *testing.T) {
	for _, item := range []struct {
		name    string
		snippet string
		terms   []string
		want    string
	}{
		{
			name:    "single term",
			snippet: "Go makes concurrency simple",
			terms:   []string{"go"},
			want:    "<mark>Go</mark> makes concurrency simple",
		},
		{
			name:    "inflected form marked to word end",
			snippet: "Crawling the web",
			terms:   []string{"crawl"},
			want:    "<mark>Crawling</mark> the web",
		},
		{
			name:    "mid-word occurrence not marked",
			snippet: "cargo ships",
			terms:   []string{"go"},
			want:    "cargo ships",
		},
		{
			name:    "multiple terms in order",
			snippet: "search the swarm search index",
			terms:   []string{"search", "swarm"},
			want:    "<mark>search</mark> the <mark>swarm</mark> <mark>search</mark> index",
		},
		{
			name:    "html in snippet is escaped",
			snippet: `<script>alert("go")</script>`,
			terms:   []string{"alert"},
			want:    "&lt;script&gt;<mark>alert</mark>(&#34;go&#34;)&lt;/script&gt;",
		},
		{
			name:    "cyrillic word start",
			snippet: "Новости про Зеленского",
			terms:   []string{"зеленск"},
			want:    "Новости про <mark>Зеленского</mark>",
		},
		{
			name:    "no terms escapes only",
			snippet: "a < b",
			terms:   nil,
			want:    "a &lt; b",
		},
		{
			name:    "blank terms ignored",
			snippet: "plain text",
			terms:   []string{"  ", ""},
			want:    "plain text",
		},
		{
			name:    "empty snippet",
			snippet: "",
			terms:   []string{"go"},
			want:    "",
		},
	} {
		if got := string(HighlightMatches(item.snippet, item.terms, nil)); got != item.want {
			t.Fatalf("%s: Highlight = %q, want %q", item.name, got, item.want)
		}
	}
}

func TestHighlightShortTermMatchesExactWordOnly(t *testing.T) {
	got := HighlightMatches("чтобы было что вспомнить", []string{"что"}, nil)
	want := template.HTML("чтобы было <mark>что</mark> вспомнить")
	if got != want {
		t.Fatalf("short-term highlight = %q, want %q", got, want)
	}
}

func TestHighlightLongTermMarksInflectedForm(t *testing.T) {
	got := HighlightMatches("прошлой осенью в горах", []string{"осень"}, nil)
	want := template.HTML("прошлой <mark>осенью</mark> в горах")
	if got != want {
		t.Fatalf("inflected highlight = %q, want %q", got, want)
	}
}

func TestHighlightMarksSiblingRussianInflections(t *testing.T) {
	got := HighlightMatches(
		"чрезвычайных полномочий передали Путину",
		[]string{"чрезвычайные", "полномочия", "Путина"},
		nil,
	)
	want := template.HTML(
		"<mark>чрезвычайных</mark> <mark>полномочий</mark> передали <mark>Путину</mark>",
	)
	if got != want {
		t.Fatalf("Russian inflection highlight = %q, want %q", got, want)
	}
}

func TestHighlightMatchesUsesValidatedAnalyzerOffsets(t *testing.T) {
	got := HighlightMatches(
		"people & <things>",
		nil,
		[]QueryMatch{{Start: 0, End: 6}, {Start: -1, End: 50}},
	)
	want := template.HTML("<mark>people</mark> &amp; &lt;things&gt;")
	if got != want {
		t.Fatalf("analyzed highlight = %q, want %q", got, want)
	}
}

func TestHighlightMatchesCoalescesOverlappingOffsets(t *testing.T) {
	got := HighlightMatches(
		"abcdef",
		nil,
		[]QueryMatch{{Start: 2, End: 4}, {Start: 0, End: 2}, {Start: 0, End: 3}},
	)
	want := template.HTML("<mark>abcd</mark>ef")
	if got != want {
		t.Fatalf("overlapping highlight = %q, want %q", got, want)
	}
}

func TestHighlightMatchesDoesNotUnionFallbackWithAnalyzerEvidence(t *testing.T) {
	got := HighlightMatches(
		"exploration spaceship",
		[]string{"explore", "space"},
		[]QueryMatch{{Start: 0, End: 11}},
	)
	want := template.HTML("<mark>exploration</mark> spaceship")
	if got != want {
		t.Fatalf("authoritative analyzer highlight = %q, want %q", got, want)
	}
}

func TestHighlightMatchesAuthoritativeEmptyEvidenceSuppressesFallback(t *testing.T) {
	got := HighlightMatches("spaceship", []string{"space"}, []QueryMatch{})
	want := template.HTML("spaceship")
	if got != want {
		t.Fatalf("empty analyzer highlight = %q, want %q", got, want)
	}
}

func TestHighlightMarksCombiningSequenceAsOneWord(t *testing.T) {
	got := HighlightMatches("שָׁלוֹם עולם", []string{"שָׁלוֹם"}, nil)
	want := template.HTML("<mark>שָׁלוֹם</mark> עולם")
	if got != want {
		t.Fatalf("combining-sequence highlight = %q, want %q", got, want)
	}
}

func TestHighlightMarksUnsegmentedScriptSubstring(t *testing.T) {
	got := HighlightMatches("K東京タワー", []string{"東京"}, nil)
	want := template.HTML("K<mark>東京</mark>タワー")
	if got != want {
		t.Fatalf("unsegmented-script highlight = %q, want %q", got, want)
	}
}

func TestHighlightRejectsUnsegmentedPrefixAffinity(t *testing.T) {
	got := HighlightMatches("東京都庁内", []string{"東京都庁舎"}, nil)
	if got != "東京都庁内" {
		t.Fatalf("unsegmented prefix highlight = %q", got)
	}
}

func TestHighlightMarksPunctuatedIdentifierAtBoundaries(t *testing.T) {
	got := HighlightMatches(
		"Use Node.js, not node, node.jsp, or capital markets.",
		[]string{"node.js", "api"},
		nil,
	)
	want := template.HTML(
		"Use <mark>Node.js</mark>, not node, node.jsp, or capital markets.",
	)
	if got != want {
		t.Fatalf("identifier highlight = %q, want %q", got, want)
	}
}

func TestHighlightRejectsAnalyzerOffsetInsideRune(t *testing.T) {
	got := HighlightMatches("я", nil, []QueryMatch{{Start: 1, End: 2}})
	if got != "я" {
		t.Fatalf("split-rune highlight = %q", got)
	}
}

func TestHighlightEscapesMarkupAroundRussianAnalyzerOffset(t *testing.T) {
	snippet := "<b>полномочий</b>"
	got := HighlightMatches(
		snippet,
		nil,
		[]QueryMatch{{Start: 3, End: 3 + len("полномочий")}},
	)
	want := template.HTML("&lt;b&gt;<mark>полномочий</mark>&lt;/b&gt;")
	if got != want {
		t.Fatalf("escaped Russian analyzer highlight = %q, want %q", got, want)
	}
}
