package pageparse_test

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
)

func TestResolveLinksSplitsLocalAndExternal(t *testing.T) {
	local, external := pageparse.ResolveLinks("https://example.com/dir/page", []string{
		"/root",
		"https://example.com/other",
		"https://elsewhere.com/x",
		"ftp://example.com/skip",
		"://bad",
	})
	wantLocal := []string{"https://example.com/root", "https://example.com/other"}
	wantExternal := []string{"https://elsewhere.com/x"}
	if !slices.Equal(local, wantLocal) {
		t.Errorf("local = %v want %v", local, wantLocal)
	}
	if !slices.Equal(external, wantExternal) {
		t.Errorf("external = %v want %v", external, wantExternal)
	}
}

func TestResolveLinksBadBase(t *testing.T) {
	local, external := pageparse.ResolveLinks("://bad", []string{"https://example.com/"})
	if local != nil || external != nil {
		t.Errorf("bad base should yield nil,nil got %v,%v", local, external)
	}
}

func TestBuildPageStats(t *testing.T) {
	stats := pageparse.BuildPageStats(pageparse.ParsedPage{
		URL:   "https://example.com/",
		Title: "Hello World",
		Text:  "some indexed words here",
		Links: []string{"https://example.com/a", "https://elsewhere.com/b"},
	})
	if len(stats.Tokens) == 0 {
		t.Error("expected tokens from text")
	}
	if len(stats.TitleTokens) == 0 {
		t.Error("expected title tokens")
	}
	if len(stats.LocalLinks) != 1 || len(stats.ExternalLinks) != 1 {
		t.Errorf("links = %v / %v", stats.LocalLinks, stats.ExternalLinks)
	}
}
