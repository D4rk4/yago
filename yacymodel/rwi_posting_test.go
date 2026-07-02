package yacymodel

import (
	"errors"
	"testing"
)

const sampleRWILine = "ABCDEFGHIJKL{c=1,h=MNOPQRSTUVWX,x=2,z=AAAAAA}"

func TestParseRWIPostingRoundTrip(t *testing.T) {
	entry, err := ParseRWIPosting(sampleRWILine)
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

func TestParseRWIPostingNormalizesYaCyPropertyForm(t *testing.T) {
	entry, err := ParseRWIPosting(
		"ABCDEFGHIJKL{c=1,d=104,h=MNOPQRSTUVWX,l=eng,t=258x,x=2,z=AAAAAAA}",
	)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Properties[ColHitCount] != "1" {
		t.Errorf("hit count = %q", entry.Properties[ColHitCount])
	}
	if entry.Properties[ColDocType] != "104" {
		t.Errorf("doctype = %q", entry.Properties[ColDocType])
	}
	if entry.Properties[ColLanguage] != "en" {
		t.Errorf("language = %q", entry.Properties[ColLanguage])
	}
	if entry.Properties[ColTextPosition] != "258" {
		t.Errorf("text position = %q", entry.Properties[ColTextPosition])
	}
	if entry.Properties[ColLocalLinkCount] != "2" {
		t.Errorf("local link count = %q", entry.Properties[ColLocalLinkCount])
	}
	if entry.Properties[ColFlags] != "AAAAAA" {
		t.Errorf("flags = %q", entry.Properties[ColFlags])
	}
}

func TestParseRWIPostingErrors(t *testing.T) {
	cases := []string{
		"ABCDEFGHIJKLnobraces",
		"short{h=MNOPQRSTUVWX}",
		"ABCDEFGHIJKL{h=MNOPQRSTUVWX,badtoken}",
		"ABCDEFGHIJKL{h=MNOPQRSTUVWX,z===}",
		"ABCDEFGHIJKL{h=short}",
	}
	for _, c := range cases {
		if _, err := ParseRWIPosting(c); !errors.Is(err, ErrBadRWIPosting) {
			t.Errorf("ParseRWIPosting(%q) = %v, want ErrBadRWIPosting", c, err)
		}
	}
}

func TestParseRWIPostingSkipsEmptyPropertyTokens(t *testing.T) {
	entry, err := ParseRWIPosting("ABCDEFGHIJKL{h=MNOPQRSTUVWX,}")
	if err != nil {
		t.Fatal(err)
	}
	if got := entry.Properties[ColURLHash]; got != "MNOPQRSTUVWX" {
		t.Fatalf("url hash property = %q", got)
	}
}
