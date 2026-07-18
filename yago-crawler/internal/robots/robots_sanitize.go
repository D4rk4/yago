package robots

import (
	"bytes"
	"strings"
)

// sanitizeRobots removes the stray directive lines a strict parser rejects: a
// directive placed before the first User-agent line belongs to no group and is
// invalid, yet real sites ship them (a leading Crawl-delay is common). Dropping
// every non-blank, non-comment line before the first User-agent lets the
// remaining groups parse, so the host's actual Disallow rules are still honored.
// Lines from the first User-agent onward are kept verbatim.
func sanitizeRobots(body []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(body))
	seenUserAgent := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		if !seenUserAgent {
			if isUserAgentLine(line) {
				seenUserAgent = true
			} else if !keepBeforeFirstGroup(line) {
				continue
			}
		}
		out.Write(line)
		out.WriteByte('\n')
	}

	return out.Bytes()
}

// isUserAgentLine reports whether a line opens a User-agent group, ignoring
// surrounding space and letter case.
func isUserAgentLine(line []byte) bool {
	field, _, ok := bytes.Cut(line, []byte(":"))

	return ok && strings.EqualFold(strings.TrimSpace(string(field)), "user-agent")
}

// keepBeforeFirstGroup reports whether a line seen before the first User-agent
// group carries no directive and is safe to keep: blank lines and comments stay,
// any other line is a stray directive and is dropped.
func keepBeforeFirstGroup(line []byte) bool {
	trimmed := bytes.TrimSpace(line)

	return len(trimmed) == 0 || trimmed[0] == '#'
}
