package crawladmission_test

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawladmission"
)

func TestAdmitLinksSkipsDisabledAndUnsupportedFormatsBeforeFrontier(t *testing.T) {
	formats := yagocrawlcontract.DefaultFormatToggles()
	profile, err := crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:          yagocrawlcontract.ScopeWide,
		URLMustMatch:   yagocrawlcontract.MatchAll,
		AllowQueryURLs: true,
		Formats:        formats,
	})
	if err != nil {
		t.Fatalf("compile profile: %v", err)
	}
	links := []string{
		"/page",
		"/release.zip",
		"/release.TAR.GZ?signature=1",
		"/setup.msi",
		"/setup.pkg",
		"/system.iso",
		"/download?file=release.zip",
	}
	want := []string{
		"https://example.com/page",
		"https://example.com/download?file=release.zip",
	}
	if got := profile.AdmitLinks("https://example.com/", links); !slices.Equal(got, want) {
		t.Fatalf("disabled-format admission = %v, want %v", got, want)
	}

	formats.Archives = true
	profile, err = crawladmission.CompileProfile(yagocrawlcontract.CrawlProfile{
		Scope:          yagocrawlcontract.ScopeWide,
		URLMustMatch:   yagocrawlcontract.MatchAll,
		AllowQueryURLs: true,
		Formats:        formats,
	})
	if err != nil {
		t.Fatalf("compile archive profile: %v", err)
	}
	want = []string{
		"https://example.com/page",
		"https://example.com/release.zip",
		"https://example.com/release.TAR.GZ?signature=1",
		"https://example.com/download?file=release.zip",
	}
	if got := profile.AdmitLinks("https://example.com/", links); !slices.Equal(got, want) {
		t.Fatalf("enabled-format admission = %v, want %v", got, want)
	}
}
