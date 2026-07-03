package docidentity_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/docidentity"
)

func TestCanonicalizeURLNormalizesEquivalentURLs(t *testing.T) {
	a, ok := docidentity.CanonicalizeURL("HTTPS://Example.com/path/?b=2&a=1&utm_source=x#frag", []string{"utm_source"})
	if !ok {
		t.Fatal("expected ok")
	}
	b, ok := docidentity.CanonicalizeURL("https://example.com/path?a=1&b=2", nil)
	if !ok {
		t.Fatal("expected ok")
	}
	if a != b {
		t.Errorf("canonical mismatch: %q != %q", a, b)
	}
}

func TestCanonicalizeURLKeepsRootSlash(t *testing.T) {
	got, ok := docidentity.CanonicalizeURL("https://example.com/", nil)
	if !ok {
		t.Fatal("expected ok")
	}
	if got != "https://example.com/" {
		t.Errorf("got %q", got)
	}
}

func TestCanonicalizeURLDistinctSchemes(t *testing.T) {
	http, _ := docidentity.CanonicalizeURL("http://example.com/", nil)
	https, _ := docidentity.CanonicalizeURL("https://example.com/", nil)
	if http == https {
		t.Error("http and https should not collapse")
	}
}

func TestCanonicalizeURLRejectsNonHTTP(t *testing.T) {
	if _, ok := docidentity.CanonicalizeURL("ftp://example.com/", nil); ok {
		t.Error("expected rejection of non-http scheme")
	}
}
