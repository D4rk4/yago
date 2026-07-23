package tavilyapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	maximumRawContentResponseBytes = 16 << 20
	maximumRawContentURLBytes      = 8 << 10
	rawContentStringHeaderBytes    = 16
	rawContentMapEntryBytes        = 64
	rawContentQueueEntryBytes      = 32
	rawContentExtractResultBytes   = 72
	rawContentExtractFailureBytes  = 32
	rawContentCrawlResultBytes     = 48
	rawContentSearchResultBytes    = 160
	rawContentSearchImageBytes     = 48
	rawContentEnvelopeBytes        = 64 << 10
	rawContentResultJSONBytes      = 96
	maximumMarkdownHeadingBytes    = 1 << 20
)

var errRawContentBudgetExceeded = errors.New("raw content response exceeds resource limit")

type rawContentBudget struct {
	retained int
	output   int
}

func (b *rawContentBudget) reserve(retained, output int) bool {
	if retained > maximumRawContentResponseBytes-b.retained ||
		output > maximumRawContentResponseBytes-b.output {
		return false
	}
	b.retained += retained
	b.output += output

	return true
}

func rawContentResponseError(
	err error,
	defaultCode string,
	badRequestCode string,
) (int, string) {
	if isBadRequest(err) {
		return http.StatusBadRequest, badRequestCode
	}
	if errors.Is(err, errRawContentBudgetExceeded) {
		return http.StatusRequestEntityTooLarge, "raw_content_too_large"
	}
	if errors.Is(err, errSearchUnavailable) {
		return http.StatusServiceUnavailable, "search_unavailable"
	}

	return http.StatusInternalServerError, defaultCode
}

func rawContentJSONStringBytes(value string) int {
	size := 2
	for len(value) > 0 {
		r, width := utf8.DecodeRuneInString(value)
		addition := width
		switch {
		case r == utf8.RuneError && width == 1:
			addition = 6
		case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' ||
			r == '\r' || r == '\t':
			addition = 2
		case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
			addition = 6
		}
		if addition > maximumRawContentResponseBytes-size {
			return maximumRawContentResponseBytes + 1
		}
		size += addition
		value = value[width:]
	}

	return size
}

func boundedFetchedMarkdown(content FetchedContent, limit int) (string, bool) {
	if content.Title == "" {
		return content.Text, len(content.Text) <= limit
	}
	if len(content.Title) > limit-4 || len(content.Text) > limit-4-len(content.Title) {
		return "", false
	}

	return "# " + content.Title + "\n\n" + content.Text, true
}

func boundedDocumentMarkdown(
	doc documentstore.Document,
	limit int,
) (string, bool) {
	headings, ok := boundedMarkdownHeadings(doc.Headings)
	if !ok {
		return "", false
	}
	out := &boundedMarkdownBuilder{limit: limit}
	if doc.Title != "" && (!out.write("# ") || !out.write(doc.Title) || !out.write("\n")) {
		return "", false
	}
	remaining := doc.ExtractedText
	for {
		line, rest, found := strings.Cut(remaining, "\n")
		if !out.writeLine(strings.TrimSpace(line), headings) {
			return "", false
		}
		if !found {
			break
		}
		remaining = rest
	}

	return strings.TrimSpace(out.text.String()), true
}

type boundedMarkdownBuilder struct {
	text  strings.Builder
	limit int
}

func (b *boundedMarkdownBuilder) write(value string) bool {
	if len(value) > b.limit-b.text.Len() {
		return false
	}
	b.text.WriteString(value)

	return true
}

func (b *boundedMarkdownBuilder) writeLine(line string, headings map[string]bool) bool {
	switch {
	case line == "":
		return true
	case headings[line]:
		return b.write("\n## ") && b.write(line) && b.write("\n")
	case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
		return b.write(line) && b.write("\n")
	default:
		return b.write("\n") && b.write(line) && b.write("\n")
	}
}

func boundedMarkdownHeadings(headings []string) (map[string]bool, bool) {
	retained := len(headings) * rawContentMapEntryBytes
	if retained > maximumMarkdownHeadingBytes {
		return nil, false
	}
	bounded := make(map[string]bool, len(headings))
	for _, heading := range headings {
		trimmed := strings.TrimSpace(heading)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > maximumMarkdownHeadingBytes-retained {
			return nil, false
		}
		retained += len(trimmed)
		bounded[strings.Clone(trimmed)] = true
	}

	return bounded, true
}
