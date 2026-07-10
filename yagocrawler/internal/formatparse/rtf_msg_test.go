package formatparse

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestParseRTFExtractsText(t *testing.T) {
	rtf := `{\rtf1\ansi{\fonttbl{\f0 Arial;}}{\colortbl;\red0\green0\blue0;}
{\info{\title Secret}}
\pard First paragraph with \'e9 accent and \u1055?\u1088? unicode.\par
Second\tab column\line third \{escaped\} \\ done.\par
{\pict 68656c6c6f}\bin5 xxxxx after binary}`
	page, parsed := Parse(
		"https://a.example/doc.rtf", "application/rtf", []byte(rtf),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed {
		t.Fatal("rtf must parse")
	}
	for _, want := range []string{
		"First paragraph with é accent", "Пр unicode.",
		"Second column", "third {escaped} \\ done.", "after binary",
	} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("rtf text missing %q in %q", want, page.Text)
		}
	}
	for _, banned := range []string{"Arial", "Secret", "68656c6c6f", "xxxxx"} {
		if strings.Contains(page.Text, banned) {
			t.Fatalf("rtf text leaked skipped group content %q: %q", banned, page.Text)
		}
	}
	if page.Title == "" {
		t.Fatal("rtf title missing")
	}

	if _, parsed := Parse(
		"https://a.example/empty.rtf", "application/rtf",
		[]byte(`{\rtf1{\fonttbl{\f0 Arial;}}}`),
		yagocrawlcontract.DefaultFormatToggles(),
	); parsed {
		t.Fatal("rtf with no body text must stay unparsed")
	}
}

func TestRTFControlWordEdges(t *testing.T) {
	word, parameter, consumed := rtfControlWord([]byte("u-4096?rest"))
	if word != "u" || parameter != -4096 || consumed != 6 {
		t.Fatalf("control word = %q %d %d", word, parameter, consumed)
	}
	if got := rtfText([]byte(`\`)); got != "" {
		t.Fatalf("trailing backslash = %q", got)
	}
	if got := rtfText([]byte(`a\~b`)); got != "ab" {
		t.Fatalf("unknown control symbol = %q", got)
	}
	if got := rtfText([]byte(`{\pict\u65?}x\u9999999?\u-64?`)); got != "x\uffc0" {
		t.Fatalf(
			"unicode escapes = %q, want skipped group + out-of-range drop + negative mapping",
			got,
		)
	}
}

// TestRTFIgnorableDestination pins the \* control symbol: it marks the
// enclosing group as an ignorable destination whose contents never leak into
// the extracted text.
func TestRTFIgnorableDestination(t *testing.T) {
	if got := rtfText([]byte(`a{\*\revtbl hidden revisions}b`)); got != "ab" {
		t.Fatalf("ignorable destination = %q, want %q", got, "ab")
	}
}

func TestParseMSGReadableRuns(t *testing.T) {
	body := make([]byte, 0, 84)
	body = append(body, 0xd0, 0xcf, 0x11, 0xe0, 0x01, 0x02)
	body = append(body, []byte("Subject: Quarterly report")...)
	body = append(body, 0x00, 0x01)
	for _, b := range []byte("Meeting agenda attached") {
		body = append(body, b, 0x00)
	}
	body = append(body, 0xff, 0xfe, 'a', 'b', 0x03)

	page, parsed := Parse(
		"https://a.example/mail.msg", "application/octet-stream", body,
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed {
		t.Fatal("msg must parse readable runs")
	}
	if !strings.Contains(page.Text, "Subject: Quarterly report") ||
		!strings.Contains(page.Text, "Meeting agenda attached") {
		t.Fatalf("msg text = %q", page.Text)
	}
	if strings.Contains(page.Text, "ab") && len(page.Text) < 40 {
		t.Fatalf("short junk run must be dropped: %q", page.Text)
	}

	if _, parsed := Parse(
		"https://a.example/junk.msg", "application/octet-stream",
		[]byte{0x00, 0x01, 0x02, 'a', 'b', 0x03},
		yagocrawlcontract.DefaultFormatToggles(),
	); parsed {
		t.Fatal("msg without readable runs must stay unparsed")
	}
}
