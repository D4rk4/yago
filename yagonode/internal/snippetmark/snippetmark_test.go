package snippetmark

import "testing"

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
		if got := string(Highlight(item.snippet, item.terms)); got != item.want {
			t.Fatalf("%s: Highlight = %q, want %q", item.name, got, item.want)
		}
	}
}
