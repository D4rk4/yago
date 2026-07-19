package adminui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSavedCrawlProfileRedirectRemainsOnAdminOrigin(t *testing.T) {
	for _, identity := range []string{"", "//outside.example/path?next=1"} {
		recorder := httptest.NewRecorder()
		redirectToSavedCrawlProfile(recorder, identity)
		response := recorder.Result()
		if response.StatusCode != http.StatusSeeOther {
			t.Fatalf("identity %q status = %d", identity, response.StatusCode)
		}
		location, err := url.Parse(response.Header.Get("Location"))
		if err != nil {
			t.Fatalf("identity %q location: %v", identity, err)
		}
		if location.IsAbs() || location.Host != "" || location.Path != crawlPath ||
			location.Query().Get("profile") != identity {
			t.Fatalf("identity %q location = %q", identity, location.String())
		}
	}
}
