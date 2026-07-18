package weburl_test

import (
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

func TestRobotsURLDerivesOrigin(t *testing.T) {
	cases := map[string]string{
		"https://example.org/":               "https://example.org/robots.txt",
		"http://example.org/a/b?q=1#frag":    "http://example.org/robots.txt",
		"https://example.org:8443/deep/path": "https://example.org:8443/robots.txt",
	}
	for input, want := range cases {
		got, ok := weburl.RobotsURL(input)
		if !ok || got != want {
			t.Fatalf("RobotsURL(%q) = %q/%v, want %q/true", input, got, ok, want)
		}
	}
}

func TestRobotsURLRejectsNonHTTP(t *testing.T) {
	for _, input := range []string{
		"ftp://example.org/x",
		"mailto:someone@example.org",
		"/relative/path",
		"https://",
		"://bad",
		"",
	} {
		if got, ok := weburl.RobotsURL(input); ok {
			t.Fatalf("RobotsURL(%q) = %q/true, want rejection", input, got)
		}
	}
}
