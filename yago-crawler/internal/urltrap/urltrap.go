// Package urltrap flags URLs whose structure marks them as a likely crawler
// trap — calendar, faceted-navigation, or path-recursion loops that mint an
// unbounded space of distinct URLs and flood the frontier — so the link
// admission gate can drop them before they are ever fetched. Session-id and
// tracking-parameter variants are already collapsed by URL normalization
// (weburl.Normalize); these are the complementary *structural* heuristics
// (Olston & Najork, "Web Crawling", FnTIR 2010): a genuine page URL is short,
// shallow, and does not repeat a path segment or carry a wall of parameters.
// The thresholds are deliberately conservative so a real deep site is admitted
// and only runaway trap shapes are refused.
package urltrap

import (
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	// maxURLLength refuses URLs longer than a browser or server would normally
	// emit; recursion traps grow the URL without bound.
	maxURLLength = yagomodel.MaximumURLIdentityBytes
	// maxPathSegments refuses pathologically deep paths; real sites rarely nest
	// beyond a handful of segments while a recursion trap deepens indefinitely.
	maxPathSegments = 24
	// maxSegmentRepeat refuses a path where any single segment recurs more than
	// this many times (e.g. /a/b/a/b/a/b/a), the signature of a link cycle.
	maxSegmentRepeat = 3
	// maxQueryParameters refuses a query carrying more parameters than faceted
	// navigation legitimately needs; a facet trap multiplies them without bound.
	maxQueryParameters = 16
)

// Suspicious reports whether a normalized http(s) URL looks like a crawler trap.
// It is a pure structural test with no I/O; a false verdict means "admit". A URL
// that fails to parse is treated as suspicious so a malformed candidate is
// dropped rather than fetched.
func Suspicious(normURL string) bool {
	if len(normURL) > maxURLLength {
		return true
	}
	parsed, err := url.Parse(normURL)
	if err != nil {
		return true
	}
	segments := pathSegments(parsed.Path)
	if len(segments) > maxPathSegments {
		return true
	}
	if repeatsSegment(segments, maxSegmentRepeat) {
		return true
	}

	return queryParameterCount(parsed.RawQuery) > maxQueryParameters
}

// pathSegments splits a URL path into its non-empty segments.
func pathSegments(rawPath string) []string {
	parts := strings.Split(rawPath, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			segments = append(segments, part)
		}
	}

	return segments
}

// repeatsSegment reports whether any single path segment occurs more than limit
// times, the signature of a path-recursion loop.
func repeatsSegment(segments []string, limit int) bool {
	counts := make(map[string]int, len(segments))
	for _, segment := range segments {
		counts[segment]++
		if counts[segment] > limit {
			return true
		}
	}

	return false
}

// queryParameterCount counts the &-separated parameters in a raw query string.
func queryParameterCount(rawQuery string) int {
	if rawQuery == "" {
		return 0
	}

	return strings.Count(rawQuery, "&") + 1
}
