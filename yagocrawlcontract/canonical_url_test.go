package yagocrawlcontract_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCanonicalURLCanonicalizesEquivalentSpellings(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want string
	}{
		"lowercase host": {
			raw:  "https://Anticisco.RU/forum/",
			want: "https://anticisco.ru/forum/",
		},
		"default port http": {
			raw:  "http://example.org:80/page",
			want: "http://example.org/page",
		},
		"default port https": {
			raw:  "https://example.org:443/page",
			want: "https://example.org/page",
		},
		"custom port kept": {
			raw:  "https://example.org:8443/page",
			want: "https://example.org:8443/page",
		},
		"empty path becomes root": {
			raw:  "https://example.org",
			want: "https://example.org/",
		},
		"dot segments removed": {
			raw:  "https://example.org/a/b/../c/./d",
			want: "https://example.org/a/c/d",
		},
		"trailing slash preserved": {
			raw:  "https://example.org/fish/",
			want: "https://example.org/fish/",
		},
		"fragment stripped": {
			raw:  "https://example.org/page#section",
			want: "https://example.org/page",
		},
		"query params sorted": {
			raw:  "https://example.org/p?b=2&a=1",
			want: "https://example.org/p?a=1&b=2",
		},
		"path case preserved": {
			raw:  "https://example.org/Page/Case",
			want: "https://example.org/Page/Case",
		},
	}
	for name, testCase := range cases {
		got, ok := yagocrawlcontract.CanonicalURL(testCase.raw)
		if !ok || got != testCase.want {
			t.Fatalf(
				"%s: CanonicalURL(%q) = %q, %v; want %q",
				name, testCase.raw, got, ok, testCase.want,
			)
		}
	}
}

func TestCanonicalURLStripsTrackingAndSessionParams(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want string
	}{
		"utm family": {
			raw:  "https://example.org/p?utm_source=x&utm_medium=y&id=7",
			want: "https://example.org/p?id=7",
		},
		"click ids": {
			raw:  "https://example.org/p?gclid=abc&fbclid=def&yclid=ghi&q=go",
			want: "https://example.org/p?q=go",
		},
		"php session id": {
			raw:  "https://example.org/p?PHPSESSID=deadbeef&x=1",
			want: "https://example.org/p?x=1",
		},
		"asp session id": {
			raw:  "https://example.org/p?ASPSESSIONIDABC=token&x=1",
			want: "https://example.org/p?x=1",
		},
		"forum sid token": {
			raw:  "https://anticisco.ru/forum/viewtopic.php?f=2&t=7253&sid=e03779eefec848544af0312d788de7d9",
			want: "https://anticisco.ru/forum/viewtopic.php?f=2&t=7253",
		},
		"semantic sid kept": {
			raw:  "https://example.org/story?sid=214510",
			want: "https://example.org/story?sid=214510",
		},
		"only tracking params leaves clean url": {
			raw:  "https://example.org/p?utm_campaign=x",
			want: "https://example.org/p",
		},
		"semantic ref kept": {
			raw:  "https://example.org/p?ref=homepage",
			want: "https://example.org/p?ref=homepage",
		},
	}
	for name, testCase := range cases {
		got, ok := yagocrawlcontract.CanonicalURL(testCase.raw)
		if !ok || got != testCase.want {
			t.Fatalf(
				"%s: CanonicalURL(%q) = %q, %v; want %q",
				name, testCase.raw, got, ok, testCase.want,
			)
		}
	}
}

func TestCanonicalURLKeepsUnparsableQueryVerbatim(t *testing.T) {
	got, ok := yagocrawlcontract.CanonicalURL("https://example.org/p?a=%zz&b=1")
	if !ok || got != "https://example.org/p?a=%zz&b=1" {
		t.Fatalf("unparsable query rewritten: %q %v", got, ok)
	}
}

func TestCanonicalURLCollapsesSessionVariantsToOneKey(t *testing.T) {
	first, _ := yagocrawlcontract.CanonicalURL(
		"https://anticisco.ru/forum/viewtopic.php?sid=3425b29528f5b6c9ace60406deadbeef&t=7253&f=2",
	)
	second, _ := yagocrawlcontract.CanonicalURL(
		"https://anticisco.ru/forum/viewtopic.php?f=2&t=7253&sid=794e68eb980323bbc64d8c8e81928b12",
	)
	if first != second {
		t.Fatalf("session variants stayed distinct: %q vs %q", first, second)
	}
}

func TestCanonicalURLRejectsUncrawlableSpellings(t *testing.T) {
	cases := map[string]string{
		"unparsable":      "http://[::1",
		"foreign scheme":  "ftp://example.org/file",
		"relative":        "/page",
		"scheme only":     "https://",
		"opaque mailto":   "mailto:someone@example.org",
		"empty authority": "https:///page",
	}
	for name, raw := range cases {
		if got, ok := yagocrawlcontract.CanonicalURL(raw); ok {
			t.Fatalf("%s: CanonicalURL(%q) = %q, admitted", name, raw, got)
		}
	}
}
