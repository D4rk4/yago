// Package landing serves the node's static landing page on GET and HEAD.
// NewEndpoint is its only surface.
package landing

import "net/http"

// NewEndpoint builds the landing handler, displaying the given human-facing build
// version (for example, "v0.0.8") when it is non-empty.
func NewEndpoint(version string) http.Handler {
	return landingEndpoint{version: version}
}
