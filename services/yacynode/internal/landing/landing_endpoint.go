package landing

import (
	_ "embed"
	"log/slog"
	"net/http"
)

//go:embed landing_page.html
var landingPageHTML []byte

const landingPageContentType = "text/html; charset=utf-8"

type landingEndpoint struct{}

func (landingEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	w.Header().Set("Content-Type", landingPageContentType)
	if _, err := w.Write(landingPageHTML); err != nil {
		slog.WarnContext(r.Context(), "landing page write failed", slog.Any("error", err))
	}
}
