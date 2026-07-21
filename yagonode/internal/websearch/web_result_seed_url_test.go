package websearch

import (
	"net/url"
	"slices"
	"testing"
)

type resultURLAdmission map[string]string

func (a resultURLAdmission) AdmitCrawlSeedURL(rawURL string) (string, bool) {
	normalized, admitted := a[rawURL]

	return normalized, admitted
}

func TestResultURLsNormalizeBeforeAdmission(t *testing.T) {
	spacedURL := " " + "https://example.test/page#first" + " "
	credentialedURL := (&url.URL{
		Scheme: "https",
		User:   url.User("fixture-user"),
		Host:   "example.test",
		Path:   "/private",
	}).String()
	admission := resultURLAdmission{
		spacedURL:                          "https://example.test/page",
		"https://example.test/page#second": "https://example.test/page",
		"http://second.example/path":       "http://second.example/path",
	}
	got := resultURLs([]Result{
		{URL: spacedURL},
		{URL: "https://example.test/page#second"},
		{URL: credentialedURL},
		{URL: "ftp://example.test/file"},
		{URL: "https:///missing-host"},
		{URL: "overlong"},
		{URL: "http://second.example/path"},
	}, admission)
	want := []string{"https://example.test/page", "http://second.example/path"}
	if !slices.Equal(got, want) {
		t.Fatalf("result URLs = %#v, want %#v", got, want)
	}
}
