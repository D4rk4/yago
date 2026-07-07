package pagefetch

import (
	"errors"
	"fmt"
	"net/http"
)

// GoneError is the page rejection for a permanently dead status — 404 Not Found
// or 410 Gone — the web's two codes for a resource that does not or no longer
// exists. It wraps ErrPageRejected so existing rejection handling keeps working,
// while the recrawl path can recognise the gone signal and tombstone the URL out
// of the index (ADR-0034; RFC 9110 §15.5.5, §15.5.11).
type GoneError struct {
	Status int
}

func (e *GoneError) Error() string {
	return fmt.Sprintf("status %d: %v", e.Status, ErrPageRejected)
}

func (e *GoneError) Unwrap() error { return ErrPageRejected }

// GoneStatus reports whether an HTTP status means the resource is permanently
// gone — 404 Not Found or 410 Gone — the only statuses that tombstone a URL. A
// transient or ambiguous status (403, 429, 5xx) is never treated as gone.
func GoneStatus(status int) bool {
	return status == http.StatusNotFound || status == http.StatusGone
}

// AsGone reports whether err is, or wraps, a GoneError and returns it when so.
// The recrawl path uses it to recognise a permanently-gone page through the fetch
// chain's error wrapping and tombstone the URL.
func AsGone(err error) (*GoneError, bool) {
	var gone *GoneError
	if errors.As(err, &gone) {
		return gone, true
	}

	return nil, false
}
