package weburl_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"https://example.com/path#frag", "https://example.com/path", true},
		{"http://example.com/", "http://example.com/", true},
		{"ftp://example.com/", "", false},
		{"https:///nohost", "", false},
		{"://bad", "", false},
	}
	for _, c := range cases {
		got, ok := weburl.Normalize(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("Normalize(%q) = %q,%v want %q,%v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestHost(t *testing.T) {
	if got := weburl.Host("https://example.com:8080/path"); got != "example.com:8080" {
		t.Errorf("Host = %q", got)
	}
	if got := weburl.Host("://bad"); got != "" {
		t.Errorf("Host(bad) = %q, want empty", got)
	}
}

func TestParseBaseRejectsInvalid(t *testing.T) {
	if _, ok := weburl.ParseBase("://bad"); ok {
		t.Error("ParseBase(bad) should fail")
	}
	if base, ok := weburl.ParseBase(
		"https://example.com/dir/page",
	); !ok ||
		base.Host != "example.com" {
		t.Fatalf("ParseBase(valid) = %v, %v", base, ok)
	}
}

func TestResolve(t *testing.T) {
	base, ok := weburl.ParseBase("https://example.com/dir/page")
	if !ok {
		t.Fatal("ParseBase failed")
	}
	if got, ok := weburl.Resolve(base, "/root"); !ok || got.String() != "https://example.com/root" {
		t.Errorf("Resolve(/root) = %v,%v", got, ok)
	}
	if _, ok := weburl.Resolve(base, "mailto:a@b.com"); ok {
		t.Error("Resolve(mailto) should reject non-http scheme")
	}
	if _, ok := weburl.Resolve(base, "://bad"); ok {
		t.Error("Resolve(bad) should reject malformed reference")
	}
}
