package yagopdf

import (
	"math"
	"strings"
	"testing"
)

func TestPDFContentNumberSyntax(t *testing.T) {
	for input, want := range map[string]float64{
		"12": 12, "+12": 12, "-12": -12, ".25": 0.25, "-.25": -0.25,
		"12.345": 12.345, "4.": 4, strings.Repeat("9", 100000): 1e9,
	} {
		got, valid := pdfContentNumberValue([]byte(input))
		if !valid || math.Abs(got-want) > 1e-10 {
			t.Fatalf("number of %d bytes = %g %t, want %g", len(input), got, valid, want)
		}
	}
	for _, input := range []string{"", "+", "-", ".", "1e3", "1..2", "12Tf", "1-2"} {
		if value, valid := pdfContentNumberValue([]byte(input)); valid {
			t.Fatalf("invalid number %q admitted as %g", input, value)
		}
	}
}

func TestPDFContentScannerBoundsAndIsolation(t *testing.T) {
	scanner := pdfContentScanner{content: []byte(
		"% ignored BT\n/F#31 12.5 (abcdef) <414243> [()] << /K (ET) >> ] BT {}",
	)}
	kinds := []pdfContentTokenKind{
		pdfContentName, pdfContentNumber, pdfContentString, pdfContentString,
		pdfContentArrayStart, pdfContentString, pdfContentArrayEnd, pdfContentDictionary,
		pdfContentArrayEnd, pdfContentOperator,
	}
	for index, want := range kinds {
		token, ok := scanner.next(2)
		if !ok || token.kind != want {
			t.Fatalf("token %d = %+v %t, want %v", index, token, ok, want)
		}
		if token.kind == pdfContentName && token.name != "F1" {
			t.Fatalf("decoded name = %q", token.name)
		}
		if token.kind == pdfContentString && len(token.value) > 2 {
			t.Fatalf("oversized operand = %d", len(token.value))
		}
	}
	if token, ok := scanner.next(2); ok {
		t.Fatalf("trailing token = %+v", token)
	}
}

func TestPDFContentScannerConsumesMalformedTokens(t *testing.T) {
	for _, content := range []string{"<< /K (unterminated)", "<nothex>", "(unterminated", "12Tf", "ID", "ID payload"} {
		scanner := pdfContentScanner{content: []byte(content)}
		for attempt := 0; ; attempt++ {
			if attempt > len(content) {
				t.Fatalf("scanner made no progress for %q", content)
			}
			if _, ok := scanner.next(8); !ok {
				break
			}
		}
	}
}

func TestPDFInlineImageEndRequiresTokenBoundaries(t *testing.T) {
	for _, content := range []string{
		"ID imageXEIsuffix EI BT (visible letters) Tj ET",
		"ID image EIsuffix EI BT (visible letters) Tj ET",
		"ID image EI",
		"ID imageXEIsuffix",
	} {
		out := newPDFTextCollector(pdfMaxTextBytes)
		pdfWriteContentText(out, []byte(content), nil)
		want := ""
		if strings.Contains(content, "BT") {
			want = "visible letters"
		}
		if strings.TrimSpace(out.String()) != want {
			t.Fatalf("inline image text = %q, want %q", out.String(), want)
		}
	}
}
