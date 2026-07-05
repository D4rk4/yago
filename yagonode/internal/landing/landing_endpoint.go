package landing

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed landing_page.html
var landingPageHTML string

var landingPageTemplate = template.Must(template.New("landing").Parse(landingPageHTML))

const landingPageContentType = "text/html; charset=utf-8"

type landingEndpoint struct {
	version string
}

type landingPageData struct {
	Version string
}

func (e landingEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	w.Header().Set("Content-Type", landingPageContentType)
	if err := landingPageTemplate.Execute(w, landingPageData{Version: e.version}); err != nil {
		slog.WarnContext(r.Context(), "landing page write failed", slog.Any("error", err))
	}
}
