package searchindex

import (
	"strings"
	"testing"
)

func TestSnippetFallsBackWhenLeadingWindowIsBlank(t *testing.T) {
	text := strings.Repeat(" ", snippetRuneCap+1) + "content"
	if got := snippet(text, "fallback"); got != "fallback" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestTextTermAnchorIgnoresBlankTermsAndMatchesCase(t *testing.T) {
	if got := firstTextTermAnchor("Needle", []string{" ", "needle"}); got != 0 {
		t.Fatalf("anchor = %d", got)
	}
}
