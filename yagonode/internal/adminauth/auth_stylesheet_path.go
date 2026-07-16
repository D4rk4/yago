package adminauth

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/httproute"
)

func RejectAuthStylesheetAliases(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httproute.CanonicalPath(r.URL.Path) == PathAuthStylesheet &&
			(r.URL.Path != PathAuthStylesheet || r.URL.EscapedPath() != PathAuthStylesheet) {
			w.Header().Set("Cache-Control", authStylesheetRejectedCache)
			http.NotFound(w, r)

			return
		}
		next.ServeHTTP(w, r)
	})
}
