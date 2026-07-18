package formatparse

import (
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

// parseRTF extracts the plain text of an RTF document by walking its control
// words: group braces and control words are dropped, \'xx hex escapes decode
// (best-effort Latin-1 for the common single-byte code pages), and the
// paragraph controls become line breaks. Binary \binN payloads are skipped.
func parseRTF(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	text := strings.TrimSpace(rtfText(body))
	if text == "" {
		return pageparse.ParsedPage{URL: rawURL}, false
	}

	return pageparse.ParsedPage{
		URL:   rawURL,
		Title: textTitle(text),
		Text:  text,
	}, true
}

// rtfWalker accumulates text while tracking group depth and the depth at
// which a skippable group (font table, picture, metadata) started.
type rtfWalker struct {
	out  strings.Builder
	deep int
	skip int
}

func newRTFWalker() *rtfWalker {
	return &rtfWalker{skip: -1}
}

func (w *rtfWalker) emitting() bool { return w.skip < 0 }

func rtfText(body []byte) string {
	walker := newRTFWalker()
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '{':
			walker.deep++
		case '}':
			if walker.skip == walker.deep {
				walker.skip = -1
			}
			walker.deep--
		case '\\':
			i = walker.handleEscape(body, i)
		default:
			if walker.emitting() && body[i] != '\r' && body[i] != '\n' {
				walker.out.WriteByte(body[i])
			}
		}
	}

	return collapseBlankRuns(walker.out.String())
}

// handleEscape processes the token starting at the backslash at index i and
// returns the index of its last consumed byte.
func (w *rtfWalker) handleEscape(body []byte, i int) int {
	if i+1 >= len(body) {
		return i
	}
	next := body[i+1]
	switch {
	case next == '\'' && i+3 < len(body):
		if w.emitting() {
			if value, err := strconv.ParseUint(string(body[i+2:i+4]), 16, 8); err == nil {
				w.out.WriteRune(rune(value))
			}
		}

		return i + 3
	case next == '\\' || next == '{' || next == '}':
		if w.emitting() {
			w.out.WriteByte(next)
		}

		return i + 1
	case next == '*':
		// An ignorable destination: readers that do not understand the
		// following control word (embedded WordML, revision data, ...) drop
		// the whole group, so its contents never leak into the text.
		if w.emitting() {
			w.skip = w.deep
		}

		return i + 1
	case isRTFLetter(next):
		word, parameter, consumed := rtfControlWord(body[i+1:])

		return w.applyControlWord(word, parameter, body, i+consumed)
	default:
		return i + 1
	}
}

// applyControlWord reacts to the control words that shape extracted text and
// returns the index of the last consumed byte.
func (w *rtfWalker) applyControlWord(word string, parameter int, body []byte, i int) int {
	switch word {
	case "par", "line", "sect", "page":
		if w.emitting() {
			w.out.WriteByte('\n')
		}
	case "tab", "cell":
		if w.emitting() {
			w.out.WriteByte(' ')
		}
	case "bin":
		if parameter > 0 {
			i += parameter
		}
	case "fonttbl", "colortbl", "stylesheet", "info", "pict", "object", "themedata":
		if w.emitting() {
			w.skip = w.deep
		}
	case "u":
		w.writeUnicodeEscape(parameter)
		// The substitution character following \uN is consumed by readers
		// that honor \ucN; skipping it matches them.
		if i+1 < len(body) && body[i+1] == '?' {
			i++
		}
	}

	return i
}

// writeUnicodeEscape emits a \uN code point; RTF encodes values above 32767
// as negatives, and anything outside the valid range is dropped.
func (w *rtfWalker) writeUnicodeEscape(parameter int) {
	if !w.emitting() {
		return
	}
	if parameter < 0 {
		parameter += 65536
	}
	if parameter >= 0 && parameter <= 0x10FFFF {
		w.out.WriteRune(rune(parameter))
	}
}

// rtfControlWord reads a control word and its optional numeric parameter,
// returning how many bytes were consumed (including one trailing space).
func rtfControlWord(rest []byte) (string, int, int) {
	i := 0
	for i < len(rest) && isRTFLetter(rest[i]) {
		i++
	}
	word := string(rest[:i])
	start := i
	if i < len(rest) && (rest[i] == '-' || rest[i] >= '0' && rest[i] <= '9') {
		i++
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
	}
	parameter, _ := strconv.Atoi(string(rest[start:i]))
	consumed := i
	if i < len(rest) && rest[i] == ' ' {
		consumed++
	}

	return word, parameter, consumed
}

func isRTFLetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

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
