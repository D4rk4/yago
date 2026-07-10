// Package modifierhint supplies the guidance shown when a search carried a
// filter operator (filetype:, site:, tld:, inurl:) but no free-text term and so
// matched nothing. YaCy retrieval walks the postings for the query's word
// hashes, so a bare filter never seeds a lookup and returns an empty page; the
// hint tells the searcher to add a keyword rather than leaving the empty result
// unexplained. Keeping bare-operator queries keyword-seeded is the YaCy-parity
// choice recorded in ADR-0042; this package is the searcher-facing half of it.
package modifierhint

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

// prompt is shown for a filter-only query that matched nothing.
const prompt = "Add a search word — a filter like filetype:, site:, tld: or " +
	"inurl: narrows matches but needs a keyword to search for " +
	"(e.g. “reports filetype:pdf”)."

// Text returns the browse guidance for a completed search, or "" when none
// applies. It fires only when the query carried one of the filter operators
// (filetype:, site:, tld:, inurl:) but no free-text term and therefore matched
// nothing: any query that supplied a term, carried no filter operator, or found
// at least one result gets no hint.
func Text(req searchcore.Request, total int) string {
	if total > 0 || len(req.Terms) > 0 {
		return ""
	}
	if req.FileType == "" && req.SiteHost == "" && req.InURL == "" && req.TLD == "" {
		return ""
	}

	return prompt
}
