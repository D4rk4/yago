// Package yagopdf extracts the embedded text of PDF documents and PostScript
// programs so the crawler and the node read both formats the same way.
package yagopdf

import "strings"

func collapseBlankRuns(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

func printableASCII(b byte) bool {
	return b >= 0x20 && b < 0x7f
}
