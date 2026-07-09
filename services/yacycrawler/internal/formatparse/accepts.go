package formatparse

import (
	"strings"
)

// Accepts reports whether a fetched document of this content type is worth
// keeping for the parser registry under the job's format toggles (CRAWL-17).
// The HTTP fetcher used to hard-reject everything but HTML, which made every
// FMT family unreachable from a crawl; the decision belongs here, where the
// families, their toggles, and the URL extension are known. Extension matches
// win over MIME for the same reason they do in Parse — and they also rescue
// an application/octet-stream response for a known extension.
func Accepts(
	rawURL, contentType string,
	toggles Toggles,
) bool {
	ext := urlExtension(rawURL)
	mime := mimeType(contentType)
	if htmlMimes[mime] || (mime == "" || strings.HasPrefix(mime, "text/")) && htmlExtensions[ext] {
		return true
	}
	registered := families()
	for _, entry := range registered {
		if entry.extensions[ext] {
			return entry.parseable(toggles)
		}
	}
	for _, entry := range registered {
		if entry.mimes[mime] {
			return entry.parseable(toggles)
		}
	}

	// Unknown types: text degrades through the HTML parser like Parse does;
	// an unknown binary type has no parser and is honestly rejected.
	return mime == "" || strings.HasPrefix(mime, "text/")
}

// parseable reports whether the family can actually produce a page right now.
func (f family) parseable(toggles Toggles) bool {
	return f.enabled(toggles) && f.parse != nil
}
