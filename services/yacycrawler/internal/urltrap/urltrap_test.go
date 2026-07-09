package urltrap_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/urltrap"
)

// TestSuspiciousAdmitsRealURLs pins that ordinary deep, faceted, and mildly
// repetitive URLs are admitted — the heuristics must not punish real sites.
func TestSuspiciousAdmitsRealURLs(t *testing.T) {
	t.Parallel()

	for _, admitted := range []string{
		"https://example.com/",
		"https://example.com/blog/2026/07/a-post",
		"https://shop.example.com/category/shoes?color=red&size=42&sort=price",
		"https://example.com/a/b/a/b", // a segment repeated only twice is fine
		"https://example.com/docs/api/v2/reference/endpoints/search",
	} {
		if urltrap.Suspicious(admitted) {
			t.Errorf("Suspicious(%q) = true, want admitted", admitted)
		}
	}
}

// TestSuspiciousFlagsTraps pins each structural trap signal: an unbounded URL,
// a pathologically deep path, a path-recursion loop, a facet-parameter
// explosion, and a URL that will not parse.
func TestSuspiciousFlagsTraps(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"over-long":       "https://example.com/" + strings.Repeat("a", 2100),
		"deep-path":       "https://example.com" + deepPath(30),
		"segment-loop":    "https://example.com/a/b/a/b/a/b/a/b",
		"facet-explosion": "https://example.com/search?" + manyParams(20),
		"malformed":       "http://%zz",
	}
	for name, raw := range cases {
		if !urltrap.Suspicious(raw) {
			t.Errorf("%s: Suspicious(%q) = false, want trapped", name, raw)
		}
	}
}

// deepPath builds a path of n distinct segments so the depth cap — not the
// repeated-segment cap — is what fires.
func deepPath(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("/s")
		b.WriteString(strconv.Itoa(i))
	}

	return b.String()
}

func manyParams(n int) string {
	parts := make([]string, 0, n)
	for i := range n {
		parts = append(parts, "k"+strconv.Itoa(i)+"=1")
	}

	return strings.Join(parts, "&")
}
