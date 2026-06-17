package yacymodel

import (
	"errors"
	"testing"
)

const sampleRWILine = "ABCDEFGHIJKL{c=AB,h=MNOPQRSTUVWX,x=CD,z=AAAAAA}"

func TestParseRWIEntryRoundTrip(t *testing.T) {
	entry, err := ParseRWIEntry(sampleRWILine)
	if err != nil {
		t.Fatal(err)
	}
	if entry.WordHash != "ABCDEFGHIJKL" {
		t.Errorf("WordHash = %q", entry.WordHash)
	}
	urlHash, err := entry.URLHash()
	if err != nil || urlHash != "MNOPQRSTUVWX" {
		t.Errorf("URLHash() = %q, %v", urlHash, err)
	}
	if got := entry.String(); got != sampleRWILine {
		t.Errorf("round trip:\n got %q\nwant %q", got, sampleRWILine)
	}
}

func TestParseRWIEntryErrors(t *testing.T) {
	cases := []string{
		"ABCDEFGHIJKLnobraces",
		"short{h=MNOPQRSTUVWX}",
		"ABCDEFGHIJKL{h=MNOPQRSTUVWX,x=ABC}",
		"ABCDEFGHIJKL{h=MNOPQRSTUVWX,z=AAAAAAA}",
	}
	for _, c := range cases {
		if _, err := ParseRWIEntry(c); !errors.Is(err, ErrBadRWIEntry) {
			t.Errorf("ParseRWIEntry(%q) = %v, want ErrBadRWIEntry", c, err)
		}
	}
}

func TestAcceptableRWILine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{sampleRWILine, true},
		{"ABCDEFGHIJKL{h=MNOPQRSTUVWX,c=1}", false},
		{"ABCDEFGHIJKL{h=MNOPQRSTUVWX,x=[B@1f}", false},
		{"ABCDEFGHIJKLnobraces", false},
		{"ABCDEFGHIJKL{h=short,x=3}", false},
	}
	for _, c := range cases {
		if got := AcceptableRWILine(c.line); got != c.want {
			t.Errorf("AcceptableRWILine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
