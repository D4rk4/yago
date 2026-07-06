package formatparse

import (
	"bytes"
	"compress/zlib"
	"io"
	"strings"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

const (
	pdfMaxStreams     = 256
	pdfMaxStreamBytes = 8 << 20
	pdfMaxTextBytes   = 1 << 20
)

// parsePDF extracts a PDF's embedded text (no OCR, like YaCy): FlateDecode
// content streams inflate through the stdlib and their BT..ET text blocks
// yield the string operands of the Tj/TJ/'/" show operators. Simple encodings
// (the vast majority of crawlable PDFs) decode readably; CID-keyed fonts
// produce no text and the document stays unparsed rather than indexing noise.
// The document Info title indexes when present. PostScript shares the literal
// extractor over its uncompressed source.
func parsePDF(rawURL, _ string, body []byte) (pageparse.ParsedPage, bool) {
	if urlExtension(rawURL) == "ps" || bytes.HasPrefix(body, []byte("%!PS")) {
		return parsePostScript(rawURL, body)
	}
	if !bytes.HasPrefix(body, []byte("%PDF")) {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	var text strings.Builder
	for _, stream := range pdfFlateStreams(body) {
		text.WriteString(pdfTextFromContent(stream))
		if text.Len() > pdfMaxTextBytes {
			break
		}
	}
	extracted := strings.TrimSpace(collapseBlankRuns(text.String()))
	if !hasIndexableText(extracted) {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	title := pdfInfoTitle(body)
	if title == "" {
		title = textTitle(extracted)
	}

	return pageparse.ParsedPage{URL: rawURL, Title: title, Text: extracted}, true
}

// pdfFlateStreams inflates every FlateDecode stream in the file, bounded.
func pdfFlateStreams(body []byte) [][]byte {
	streams := make([][]byte, 0, 8)
	at := 0
	for len(streams) < pdfMaxStreams {
		index := bytes.Index(body[at:], []byte("stream"))
		if index < 0 {
			break
		}
		start := at + index + len("stream")
		if start < len(body) && body[start] == '\r' {
			start++
		}
		if start < len(body) && body[start] == '\n' {
			start++
		}
		end := bytes.Index(body[start:], []byte("endstream"))
		if end < 0 {
			break
		}
		raw := body[start : start+end]
		at = start + end + len("endstream")
		reader, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		inflated, err := io.ReadAll(io.LimitReader(reader, pdfMaxStreamBytes))
		_ = reader.Close()
		if err != nil || len(inflated) == 0 {
			continue
		}
		streams = append(streams, inflated)
	}

	return streams
}

// pdfTextFromContent walks a content stream's BT..ET blocks collecting the
// string operands of the text-show operators.
func pdfTextFromContent(content []byte) string {
	var out strings.Builder
	at := 0
	for {
		begin := bytes.Index(content[at:], []byte("BT"))
		if begin < 0 {
			break
		}
		blockStart := at + begin + 2
		end := bytes.Index(content[blockStart:], []byte("ET"))
		if end < 0 {
			break
		}
		writeShownStrings(&out, content[blockStart:blockStart+end])
		out.WriteByte('\n')
		at = blockStart + end + 2
	}

	return out.String()
}

// writeShownStrings emits every (string) literal in a text block; the show
// operators are the only place literals appear inside BT..ET.
func writeShownStrings(out *strings.Builder, block []byte) {
	for at := 0; at < len(block); at++ {
		if block[at] != '(' {
			continue
		}
		literal, consumed := pdfStringLiteral(block[at:])
		out.WriteString(literal)
		out.WriteByte(' ')
		at += consumed
	}
}

// pdfStringLiteral decodes one parenthesized literal starting at data[0]=='(',
// handling escapes and nested parentheses, returning the text and the bytes
// consumed.
func pdfStringLiteral(data []byte) (string, int) {
	var out strings.Builder
	depth := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\\':
			if i+1 < len(data) {
				out.WriteByte(pdfEscape(data[i+1]))
				i++
			}
		case '(':
			depth++
			if depth > 1 {
				out.WriteByte('(')
			}
		case ')':
			depth--
			if depth == 0 {
				return out.String(), i
			}
			out.WriteByte(')')
		default:
			if printableASCII(data[i]) || data[i] >= 0xA0 {
				out.WriteByte(data[i])
			}
		}
	}

	return out.String(), len(data)
}

func pdfEscape(b byte) byte {
	switch b {
	case 'n', 'r', 't':
		return ' '
	default:
		return b
	}
}

// hasIndexableText requires a minimum of real letters so CID-font garbage
// does not index.
func hasIndexableText(text string) bool {
	letters := 0
	for _, r := range text {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			letters++
		}
		if letters >= 8 {
			return true
		}
	}

	return false
}

// pdfInfoTitle reads the document-info /Title literal when present.
func pdfInfoTitle(body []byte) string {
	index := bytes.Index(body, []byte("/Title"))
	if index < 0 {
		return ""
	}
	rest := body[index+len("/Title"):]
	open := bytes.IndexByte(rest, '(')
	if open < 0 || open > 8 {
		return ""
	}
	title, _ := pdfStringLiteral(rest[open:])

	return strings.TrimSpace(title)
}

// parsePostScript extracts the parenthesized text literals of a PostScript
// program — the operands of its show operators.
func parsePostScript(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	if !bytes.HasPrefix(body, []byte("%!")) {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	var out strings.Builder
	writeShownStrings(&out, body)
	extracted := strings.TrimSpace(collapseBlankRuns(out.String()))
	if !hasIndexableText(extracted) {
		return pageparse.ParsedPage{URL: rawURL}, false
	}

	return pageparse.ParsedPage{
		URL:   rawURL,
		Title: textTitle(extracted),
		Text:  extracted,
	}, true
}
