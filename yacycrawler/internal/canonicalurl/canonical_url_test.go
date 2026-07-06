package canonicalurl_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/canonicalurl"
)

func TestCanonicalize(t *testing.T) {
	cases := map[string]string{
		"HTTP://Example.COM":         "http://example.com/",
		"http://example.com:80/a":    "http://example.com/a",
		"https://example.com:443/a":  "https://example.com/a",
		"http://example.com/a/../b":  "http://example.com/b",
		"http://example.com/a/#frag": "http://example.com/a/",
		"http://example.com:8080/x":  "http://example.com:8080/x",
		"http://example.com/a/b/":    "http://example.com/a/b/",
	}
	for input, want := range cases {
		got, err := canonicalurl.Canonicalize(input)
		if err != nil {
			t.Fatalf("Canonicalize(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("Canonicalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCanonicalizeRejectsBadInput(t *testing.T) {
	for _, input := range []string{"::bad", "ftp://example.com/x", "http:///path"} {
		if _, err := canonicalurl.Canonicalize(input); err == nil {
			t.Errorf("Canonicalize(%q) should error", input)
		}
	}
}

func TestResolveReference(t *testing.T) {
	got, err := canonicalurl.ResolveReference("http://example.com/dir/page", "../other")
	if err != nil {
		t.Fatalf("ResolveReference: %v", err)
	}
	if got != "http://example.com/other" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveReferenceRejectsBad(t *testing.T) {
	if _, err := canonicalurl.ResolveReference("::bad", "x"); err == nil {
		t.Error("bad base should error")
	}
	if _, err := canonicalurl.ResolveReference("http://h/", "::bad"); err == nil {
		t.Error("bad ref should error")
	}
}
