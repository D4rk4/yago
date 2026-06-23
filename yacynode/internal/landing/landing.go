// Package landing serves the node's static landing page on GET and HEAD.
// NewEndpoint is its only surface.
package landing

import "net/http"

func NewEndpoint() http.Handler {
	return landingEndpoint{}
}
