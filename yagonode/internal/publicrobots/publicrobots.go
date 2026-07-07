// Package publicrobots serves the node's own /robots.txt — YaCy's
// ConfigRobotsTxt_p parity. A public search page is an infinite crawl space
// (every query mints a new URL), so by default foreign spiders are told to
// skip the SERPs while the landing page stays indexable; the operator can
// open everything or close the whole site.
package publicrobots

import (
	"net/http"
	"strings"
)

// Policy names how much of the public surface foreign crawlers may index.
type Policy string

const (
	// PolicyOpen allows everything.
	PolicyOpen Policy = "open"
	// PolicyNoSERP (the default) hides the search result pages and the
	// suggest API — the infinite, query-addressed surfaces — while leaving
	// the landing page indexable.
	PolicyNoSERP Policy = "no-serp"
	// PolicyClosed disallows the whole site.
	PolicyClosed Policy = "closed"
)

// ParsePolicy normalizes a stored policy value; unknown values fall back to
// the default so a typo never accidentally opens the SERPs.
func ParsePolicy(raw string) Policy {
	switch Policy(strings.ToLower(strings.TrimSpace(raw))) {
	case PolicyOpen:
		return PolicyOpen
	case PolicyClosed:
		return PolicyClosed
	default:
		return PolicyNoSERP
	}
}

// serpDisallows lists the query-addressed public paths hidden by PolicyNoSERP.
// The /*?q= patterns use the wildcard syntax every major crawler honors, so
// portal searches on the site root are excluded without hiding the root.
var serpDisallows = []string{
	"/yacysearch.html",
	"/yacysearch.json",
	"/yacysearch.rss",
	"/suggest.json",
	"/api/",
	"/*?q=",
}

// Body renders the robots.txt payload for one policy.
func Body(policy Policy) string {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	switch policy {
	case PolicyOpen:
		b.WriteString("Disallow:\n")
	case PolicyClosed:
		b.WriteString("Disallow: /\n")
	default:
		for _, path := range serpDisallows {
			b.WriteString("Disallow: " + path + "\n")
		}
	}

	return b.String()
}

// Mount serves /robots.txt on the public mux; currentPolicy is read per
// request so a runtime settings change applies without a restart.
func Mount(mux *http.ServeMux, currentPolicy func() Policy) {
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(Body(currentPolicy())))
	})
}
