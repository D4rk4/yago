package adminauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthStylesheetAliasesAreRejectedBeforeServeMuxRedirects(t *testing.T) {
	t.Parallel()

	service, _ := scriptedService(t)
	surface := RejectAuthStylesheetAliases(htmlSurface(t, service))
	for _, target := range []string{
		"/admin/./auth.css",
		"/admin//auth.css",
		"/admin/%61uth.css",
		"/admin%2fauth.css",
		"/admin/auth.css/",
	} {
		t.Run(target, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
			response := httptest.NewRecorder()
			surface.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound || response.Header().Get("Location") != "" ||
				response.Header().Get("Cache-Control") != authStylesheetRejectedCache {
				t.Fatalf("alias %q = %d %#v", target, response.Code, response.Header())
			}
		})
	}

	exact := httptest.NewRecorder()
	surface.ServeHTTP(
		exact,
		httptest.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			authStylesheetReference,
			nil,
		),
	)
	if exact.Code != http.StatusOK || exact.Header().Get("Cache-Control") != authStylesheetCache {
		t.Fatalf("exact stylesheet = %d %#v", exact.Code, exact.Header())
	}
}
