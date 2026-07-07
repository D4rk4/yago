package pagefetch

import (
	"mime"
	"strings"
)

// AllowedContentType reports whether a rendered document's Content-Type is one the
// BROWSER fetcher may return — the headless browser produces HTML, so anything
// else coming out of it is a render failure. The plain HTTP fetcher accepts
// every type; per-job filtering happens in the pipeline against the format
// registry (CRAWL-17). It also reports whether a fetched document's Content-Type is one the
// crawler will parse and index. The crawler only handles HTML, so non-HTML media
// (PDF, images, archives, and so on) are rejected. It is the single MIME policy
// shared by the fast HTTP fetch path and the browser fallback, so neither can
// admit media the other would refuse.
func AllowedContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		mediaType, _, _ = strings.Cut(value, ";")
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "text/html", "application/xhtml+xml":
		return true
	default:
		return false
	}
}
