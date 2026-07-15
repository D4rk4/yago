package publicportal

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPortalOpenSearchAdvertisementMatchesDescription(t *testing.T) {
	SetGreetingProvider(func() string { return "YaGoSeek" })
	t.Cleanup(func() { SetGreetingProvider(func() string { return "" }) })

	status, body := get(t, New(&fakeSource{}, false), "https://node.example/")
	if status != http.StatusOK {
		t.Fatalf("portal status = %d", status)
	}
	advertisement := `<link rel="search" type="application/opensearchdescription+xml" ` +
		`title="YaGoSeek search" href="/opensearch.xml">`
	if !strings.Contains(body, advertisement) {
		t.Fatalf("portal has no matching OpenSearch advertisement: %s", body)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"https://node.example/opensearch.xml",
		nil,
	)
	NewOpenSearch().Describe(recorder, request)
	if contentType := recorder.Header().Get("Content-Type"); contentType != osddContentType {
		t.Fatalf("description content type = %q, want %q", contentType, osddContentType)
	}
	var description openSearchDescription
	if err := xml.Unmarshal(recorder.Body.Bytes(), &description); err != nil {
		t.Fatalf("parse OpenSearch description: %v", err)
	}
	if description.ShortName != "YaGoSeek search" {
		t.Fatalf("description ShortName = %q, want YaGoSeek search", description.ShortName)
	}
	if len([]rune(description.ShortName)) > openSearchTitleLimit {
		t.Fatalf("ShortName = %q, above %d characters", description.ShortName, openSearchTitleLimit)
	}
	if !hasOpenSearchResultURL(description, "https://node.example/?q={searchTerms}") {
		t.Fatalf("description has no HTML search URL: %#v", description.URLs)
	}
}

func TestOpenSearchTitleBoundsPortalNames(t *testing.T) {
	for name, test := range map[string]struct {
		brand string
		want  string
	}{
		"default":   {brand: "yago", want: "yago search"},
		"preferred": {brand: "YaGoSeek", want: "YaGoSeek search"},
		"name only": {brand: "My search portal", want: "My search portal"},
		"unicode":   {brand: "Моя библиотека", want: "Моя библиотека"},
		"truncated": {brand: "1234567890abcdefghi", want: "1234567890abcdef"},
		"empty":     {brand: " ", want: "yago search"},
	} {
		t.Run(name, func(t *testing.T) {
			got := openSearchTitle(test.brand)
			if got != test.want {
				t.Fatalf("openSearchTitle(%q) = %q, want %q", test.brand, got, test.want)
			}
			if len([]rune(got)) > openSearchTitleLimit {
				t.Fatalf(
					"openSearchTitle(%q) = %q, above %d characters",
					test.brand,
					got,
					openSearchTitleLimit,
				)
			}
		})
	}
}

func hasOpenSearchResultURL(description openSearchDescription, want string) bool {
	for _, endpoint := range description.URLs {
		if endpoint.Type == resultLinkType && endpoint.Template == want {
			return true
		}
	}

	return false
}
