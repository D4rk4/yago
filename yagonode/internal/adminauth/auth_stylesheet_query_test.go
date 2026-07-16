package adminauth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthStylesheetRejectsNoncanonicalRevisionQueries(t *testing.T) {
	t.Parallel()

	service, _ := scriptedService(t)
	surface := htmlSurface(t, service)
	encodedRevision := fmt.Sprintf(
		"v=%%%02X%s",
		authStylesheetRevision[0],
		authStylesheetRevision[1:],
	)
	queries := map[string]string{
		"malformed":          "v=%zz",
		"duplicate":          "v=" + authStylesheetRevision + "&v=" + authStylesheetRevision,
		"extra":              "v=" + authStylesheetRevision + "&mode=old",
		"encoded equivalent": encodedRevision,
	}
	for name, rawQuery := range queries {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				PathAuthStylesheet,
				nil,
			)
			request.URL.RawQuery = rawQuery
			response := httptest.NewRecorder()
			surface.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound ||
				response.Header().Get("Cache-Control") != authStylesheetRejectedCache ||
				response.Header().Get("X-Content-Type-Options") != "nosniff" ||
				response.Header().Get("Location") != "" {
				t.Fatalf("query %q = %d %#v", rawQuery, response.Code, response.Header())
			}
		})
	}
}
