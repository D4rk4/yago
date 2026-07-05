// Package cachedpage serves read-only cached copies of locally stored pages on
// the public listener: the text this node extracted at crawl time, rendered
// escaped, so a searcher can inspect a result without visiting the origin site.
package cachedpage

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// Path is the cached-copy endpoint; URLFor builds links to it.
const Path = "/cached"

// URLFor returns the cached-copy link for a result URL, suitable for templates.
func URLFor(rawURL string) string {
	return Path + "?u=" + template.URLQueryEscaper(rawURL)
}

var page = template.Must(template.New("cached").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="robots" content="noindex">
<title>Cached copy of {{.Title}}</title>
</head>
<body>
<main>
<p><strong>Cached copy.</strong> This is the text this node stored when it fetched the
page{{if .Fetched}} on {{.Fetched}}{{end}}; the live page may differ or be gone.
<a href="{{.URL}}" rel="noreferrer nofollow">Visit the live page</a>.</p>
<h1>{{.Title}}</h1>
<p><code>{{.URL}}</code></p>
{{range .Paragraphs}}<p>{{.}}</p>
{{end}}
</main>
</body>
</html>`))

type cachedView struct {
	Title      string
	URL        string
	Fetched    string
	Paragraphs []string
}

type endpoint struct {
	documents documentstore.DocumentDirectory
}

// Mount serves GET /cached?u=<url> from the stored document directory. A nil
// directory leaves the route unmounted so the link target 404s cleanly.
func Mount(mux *http.ServeMux, documents documentstore.DocumentDirectory) {
	if documents == nil {
		return
	}
	mux.Handle("GET "+Path, endpoint{documents: documents})
}

func (e endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("u"))
	if rawURL == "" {
		http.Error(w, "missing u parameter", http.StatusBadRequest)

		return
	}
	doc, found, err := e.documents.Document(r.Context(), rawURL)
	if err != nil {
		slog.WarnContext(r.Context(), "cached copy lookup failed", slog.Any("error", err))
		http.Error(w, "cached copy unavailable", http.StatusInternalServerError)

		return
	}
	if !found {
		http.Error(w, "no cached copy of this page", http.StatusNotFound)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := page.Execute(w, viewOf(doc)); err != nil {
		slog.WarnContext(context.WithoutCancel(r.Context()),
			"cached copy render failed", slog.Any("error", err))
	}
}

func viewOf(doc documentstore.Document) cachedView {
	title := doc.Title
	if title == "" {
		title = doc.NormalizedURL
	}
	fetched := ""
	if !doc.FetchedAt.IsZero() {
		fetched = doc.FetchedAt.UTC().Format(time.RFC3339)
	}

	return cachedView{
		Title:      title,
		URL:        doc.NormalizedURL,
		Fetched:    fetched,
		Paragraphs: paragraphsOf(doc.ExtractedText),
	}
}

// paragraphsOf splits the stored extracted text on blank lines so the cached
// page reads as paragraphs; html/template escapes every fragment on render.
func paragraphsOf(text string) []string {
	blocks := strings.Split(text, "\n\n")
	paragraphs := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if trimmed := strings.TrimSpace(block); trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}

	return paragraphs
}
