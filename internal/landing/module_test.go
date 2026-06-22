package landing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLandingEndpointServesHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

	New().Endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != landingPageContentType {
		t.Errorf("content type = %q, want %q", got, landingPageContentType)
	}
	body := rec.Body.String()
	for _, want := range []string{"alpha", "RWI", "github.com/nikitakarpei/yacy-rwi-node/issues"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestLandingEndpointRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)

	New().Endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("allow = %q, want %q", got, http.MethodGet)
	}
}
