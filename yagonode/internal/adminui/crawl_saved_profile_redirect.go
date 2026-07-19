package adminui

import (
	"net/http"
	"net/url"
)

func redirectToSavedCrawlProfile(w http.ResponseWriter, identity string) {
	target := url.URL{Path: crawlPath}
	if identity != "" {
		target.RawQuery = url.Values{"profile": []string{identity}}.Encode()
	}
	w.Header().Set("Location", target.String())
	w.WriteHeader(http.StatusSeeOther)
}
