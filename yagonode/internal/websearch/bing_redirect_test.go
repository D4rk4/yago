package websearch

import (
	"encoding/base64"
	"testing"
)

func bingClickURL(tb testing.TB, target string) string {
	tb.Helper()

	return "https://www.bing.com/ck/a?!&&p=deadbeef&u=" +
		bingRedirectVersionPrefix + base64.RawURLEncoding.EncodeToString([]byte(target))
}

func TestDecodeBingRedirectRecoversDestination(t *testing.T) {
	target := "https://hristianstvo.neocities.org/orthodoxy/"
	if got := decodeBingRedirect(bingClickURL(t, target)); got != target {
		t.Fatalf("decoded = %q, want %q", got, target)
	}
}

func TestDecodeBingRedirectPassesOrdinaryURLsThrough(t *testing.T) {
	for _, raw := range []string{
		"https://example.org/page",
		"https://www.bing.com/search?q=x",
		"https://notbing.com/ck/a?u=a1xxx",
		"",
	} {
		if got := decodeBingRedirect(raw); got != raw {
			t.Fatalf("decodeBingRedirect(%q) = %q, want passthrough", raw, got)
		}
	}
}

func TestDecodeBingRedirectDropsUnrecoverableRedirects(t *testing.T) {
	for name, raw := range map[string]string{
		"missing u param":     "https://www.bing.com/ck/a?!&&p=deadbeef",
		"unknown version":     "https://www.bing.com/ck/a?u=b2QQQQ",
		"broken base64":       "https://www.bing.com/ck/a?u=a1%%%",
		"invalid base64 rune": "https://www.bing.com/ck/a?u=a1!!!!",
		"relative target": "https://www.bing.com/ck/a?u=a1" +
			base64.RawURLEncoding.EncodeToString([]byte("/relative/path")),
		"unparsable target": "https://www.bing.com/ck/a?u=a1" +
			base64.RawURLEncoding.EncodeToString([]byte("http://%zz")),
	} {
		if got := decodeBingRedirect(raw); got != "" {
			t.Fatalf("%s: decoded = %q, want dropped", name, got)
		}
	}
}

func TestDecodeBingRedirectUnparsableURLPassesThrough(t *testing.T) {
	raw := "http://%zz"
	if got := decodeBingRedirect(raw); got != raw {
		t.Fatalf("unparsable URL = %q, want passthrough", got)
	}
}
