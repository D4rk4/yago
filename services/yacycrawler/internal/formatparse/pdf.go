package formatparse

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
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
func parsePDF(rawURL, _ string, body []byte) (Document, bool) {
	if urlExtension(rawURL) == "ps" || bytes.HasPrefix(body, []byte("%!PS")) {
		return parsePostScript(rawURL, body)
	}
	if !bytes.HasPrefix(body, []byte("%PDF")) {
		return Document{URL: rawURL}, false
	}
	var text strings.Builder
	for _, stream := range pdfContentStreams(body) {
		text.WriteString(pdfTextFromContent(stream))
		if text.Len() > pdfMaxTextBytes {
			break
		}
	}
	extracted := strings.TrimSpace(collapseBlankRuns(text.String()))
	if !hasIndexableText(extracted) {
		return Document{URL: rawURL}, false
	}
	title := pdfInfoTitle(body)
	if title == "" {
		title = textTitle(extracted)
	}

	return Document{URL: rawURL, Title: title, Text: extracted}, true
}

// pdfContentStreams decodes every content stream in the file, bounded. The
// stream dictionary's /Filter entry names the decode chain; besides the
// modern FlateDecode this covers the 1990s combination ASCII85Decode +
// LZWDecode (real archives are full of PDF 1.0 files built exactly that
// way — CRAWL-17 follow-up) and ASCIIHexDecode. Image filters (DCTDecode,
// JPXDecode) carry no text and are skipped.
func pdfContentStreams(body []byte) [][]byte {
	streams := make([][]byte, 0, 8)
	at := 0
	for len(streams) < pdfMaxStreams {
		index := bytes.Index(body[at:], []byte("stream"))
		if index < 0 {
			break
		}
		dictStart := bytes.LastIndex(body[at:at+index], []byte("<<"))
		filters := []string{"FlateDecode"}
		if dictStart >= 0 {
			filters = pdfFilterChain(body[at+dictStart : at+index])
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
		decoded, ok := pdfDecodeChain(raw, filters)
		if !ok || len(decoded) == 0 {
			continue
		}
		streams = append(streams, decoded)
	}

	return streams
}

// pdfFilterChain reads the /Filter entry of one stream dictionary in order.
func pdfFilterChain(dict []byte) []string {
	index := bytes.Index(dict, []byte("/Filter"))
	if index < 0 {
		return []string{"FlateDecode"}
	}
	rest := dict[index+len("/Filter"):]
	if closing := bytes.IndexByte(
		rest,
		']',
	); bytes.Contains(
		rest[:min(len(rest), 2)],
		[]byte("["),
	) ||
		(closing >= 0 && bytes.IndexByte(rest[:closing], '[') >= 0) {
		rest = rest[:closing]
	}
	names := pdfNamePattern.FindAllSubmatch(rest, 4)
	filters := make([]string, 0, len(names))
	for _, name := range names {
		filters = append(filters, string(name[1]))
	}
	if len(filters) == 0 {
		return []string{"FlateDecode"}
	}

	return filters
}

var pdfNamePattern = regexp.MustCompile(`/([A-Za-z0-9]+)`)

// pdfDecodeChain applies the named filters in order; an unknown or image
// filter aborts the stream (ok=false) rather than emitting garbage.
func pdfDecodeChain(raw []byte, filters []string) ([]byte, bool) {
	data := raw
	for _, filter := range filters {
		var err error
		switch filter {
		case "FlateDecode", "Fl":
			data, err = pdfInflate(data)
		case "ASCII85Decode", "A85":
			data, err = pdfASCII85(data)
		case "ASCIIHexDecode", "AHx":
			data, err = pdfASCIIHex(data)
		case "LZWDecode", "LZW":
			data, err = pdfLZW(data)
		default:
			return nil, false
		}
		if err != nil {
			return nil, false
		}
	}

	return data, true
}

func pdfInflate(raw []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("zlib: %w", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, pdfMaxStreamBytes))
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("inflate: %w", err)
	}

	return data, nil
}

func pdfASCII85(raw []byte) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	raw = bytes.TrimSuffix(raw, []byte("~>"))
	decoder := ascii85.NewDecoder(bytes.NewReader(raw))
	data, err := io.ReadAll(io.LimitReader(decoder, pdfMaxStreamBytes))
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("ascii85: %w", err)
	}

	return data, nil
}

func pdfASCIIHex(raw []byte) ([]byte, error) {
	compact := make([]byte, 0, len(raw))
	for _, char := range raw {
		switch char {
		case '>', ' ', '\n', '\r', '\t':
		default:
			compact = append(compact, char)
		}
	}
	if len(compact)%2 == 1 {
		compact = append(compact, '0')
	}
	data := make([]byte, hex.DecodedLen(len(compact)))
	if _, err := hex.Decode(data, compact); err != nil {
		return nil, fmt.Errorf("asciihex: %w", err)
	}

	return data, nil
}

// pdfLZW decodes PDF's LZW variant; Go's MSB reader matches the common
// EarlyChange=1 encoding real files use.
func pdfLZW(raw []byte) ([]byte, error) {
	reader := lzw.NewReader(bytes.NewReader(raw), lzw.MSB, 8)
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, pdfMaxStreamBytes))
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("lzw: %w", err)
	}

	return data, nil
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
func parsePostScript(rawURL string, body []byte) (Document, bool) {
	if !bytes.HasPrefix(body, []byte("%!")) {
		return Document{URL: rawURL}, false
	}
	var out strings.Builder
	writeShownStrings(&out, body)
	extracted := strings.TrimSpace(collapseBlankRuns(out.String()))
	if !hasIndexableText(extracted) {
		return Document{URL: rawURL}, false
	}

	return Document{
		URL:   rawURL,
		Title: textTitle(extracted),
		Text:  extracted,
	}, true
}
