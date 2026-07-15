package adminauth

import "net/http"

const (
	PathAuthStylesheet  = "/admin/auth.css"
	authStylesheetCache = "public, max-age=86400"
)

func serveAuthStylesheet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", authStylesheetCache)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFileFS(w, r, authTemplateFS, "assets/auth.css")
}
