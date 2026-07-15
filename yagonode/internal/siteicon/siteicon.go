package siteicon

import "net/http"

const (
	Path                = "/favicon.svg"
	LegacyPath          = "/favicon.ico"
	siteIconCachePolicy = "public, max-age=86400"
	siteIconSVG         = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="6" fill="#161616"/><path d="M6 8h5l5 8 5-8h5l-8 12v6h-4v-6z" fill="#fa4d56"/></svg>`
)

func Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET "+Path, serveSiteIcon)
	mux.HandleFunc("GET "+LegacyPath, serveSiteIcon)
}

func serveSiteIcon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", siteIconCachePolicy)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(siteIconSVG))
}
