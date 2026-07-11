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
	"unicode/utf16"

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
	tables := pdfToUnicodeTables(body)
	var text strings.Builder
	for _, stream := range pdfContentStreams(body) {
		text.WriteString(pdfTextFromContent(stream, tables))
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
// Only the entry's value is read — a bare name or a bracketed array — so the
// keys that follow it (typically /Length) can never be mistaken for filters
// and abort the decode; that misread silently dropped every stream whose
// dictionary listed /Filter first, which is how pdfTeX writes them.
func pdfFilterChain(dict []byte) []string {
	index := bytes.Index(dict, []byte("/Filter"))
	if index < 0 {
		return []string{"FlateDecode"}
	}
	rest := bytes.TrimLeft(dict[index+len("/Filter"):], " \t\r\n")
	if len(rest) > 0 && rest[0] == '[' {
		if closing := bytes.IndexByte(rest, ']'); closing >= 0 {
			rest = rest[:closing]
		}
		names := pdfNamePattern.FindAllSubmatch(rest, 4)
		filters := make([]string, 0, len(names))
		for _, name := range names {
			filters = append(filters, string(name[1]))
		}
		if len(filters) > 0 {
			return filters
		}

		return []string{"FlateDecode"}
	}
	if name := pdfLeadingNamePattern.FindSubmatch(rest); name != nil {
		return []string{string(name[1])}
	}

	return []string{"FlateDecode"}
}

var (
	pdfNamePattern        = regexp.MustCompile(`/([A-Za-z0-9]+)`)
	pdfLeadingNamePattern = regexp.MustCompile(`^/([A-Za-z0-9]+)`)
)

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
// string operands of the text-show operators; the font selected by Tf carries
// across blocks the way the graphics state does, so a page-long font choice
// keeps its ToUnicode table.
func pdfTextFromContent(content []byte, tables map[string]*pdfCMap) string {
	var out strings.Builder
	var current *pdfCMap
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
		current = writeShownStrings(&out, content[blockStart:blockStart+end], tables, current)
		out.WriteByte('\n')
		at = blockStart + end + 2
	}

	return out.String()
}

// writeShownStrings emits every string operand in a text block: parenthesized
// literals, <hex> strings, and the elements of [..]TJ arrays. Inside an array
// a fragment follows its neighbor without a space — TeX splits words around
// kerns like [(h)31(ydrogen)] — unless the kern is large enough to be an
// inter-word gap (pdfArrayKernSpace); << dictionaries are skipped so marked-
// content properties never leak into the text. A /Name selects that font's
// ToUnicode table for the strings that follow, and the selection is returned
// so it survives into the next text block.
func writeShownStrings(
	out *strings.Builder,
	block []byte,
	tables map[string]*pdfCMap,
	current *pdfCMap,
) *pdfCMap {
	inArray := false
	for at := 0; at < len(block); at++ {
		switch block[at] {
		case '(':
			raw, consumed := pdfRawStringLiteral(block[at:])
			out.WriteString(pdfShownText(raw, current))
			at += consumed
			if !inArray {
				out.WriteByte(' ')
			}
		case '<':
			if at+1 < len(block) && block[at+1] == '<' {
				at += pdfSkipDictionary(block[at:])

				continue
			}
			raw, consumed := pdfRawHexString(block[at:])
			out.WriteString(pdfShownText(raw, current))
			at += consumed
			if !inArray {
				out.WriteByte(' ')
			}
		case '[':
			inArray = true
		case ']':
			inArray = false
			out.WriteByte(' ')
		case '-':
			if consumed, space := pdfArrayKernSpace(block[at:], inArray); space {
				out.WriteByte(' ')
				at += consumed
			}
		case '/':
			name, consumed := pdfName(block[at:])
			if selected, ok := tables[name]; ok {
				current = selected
			}
			at += consumed
		}
	}

	return current
}

// pdfShownText renders one show-string's raw bytes through the selected
// font's ToUnicode table, or through the byte decoder when none is selected.
func pdfShownText(raw []byte, current *pdfCMap) string {
	if current != nil {
		return pdfMapString(raw, current)
	}

	return pdfDecodeStringBytes(raw)
}

// pdfName reads the name token starting at data[0]=='/'.
func pdfName(data []byte) (string, int) {
	end := 1
	for ; end < len(data); end++ {
		b := data[end]
		if b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' ||
			b == '_' || b == '.' {
			continue
		}

		break
	}

	return string(data[1:end]), end - 1
}

// pdfArrayKernSpace reads the negative number starting at data[0]=='-' and
// reports whether it is an inter-word kern: TJ offsets are thousandths of an
// em, small values glue a word's fragments, values beyond -180 are gaps.
func pdfArrayKernSpace(data []byte, inArray bool) (int, bool) {
	if !inArray {
		return 0, false
	}
	value := 0
	end := 1
	for ; end < len(data) && data[end] >= '0' && data[end] <= '9'; end++ {
		if value < 10000 {
			value = value*10 + int(data[end]-'0')
		}
	}

	return end - 1, value >= 180
}

// pdfSkipDictionary returns the offset just past the <<..>> dictionary that
// starts at data[0]; an unterminated dictionary consumes the rest.
func pdfSkipDictionary(data []byte) int {
	if end := bytes.Index(data, []byte(">>")); end >= 0 {
		return end + 1
	}

	return len(data)
}

// pdfHexString decodes one <..> hex string starting at data[0]=='<' through
// the byte decoder; odd-length runs pad their final nibble per the PDF rules.
func pdfHexString(data []byte) (string, int) {
	raw, consumed := pdfRawHexString(data)

	return pdfDecodeStringBytes(raw), consumed
}

// pdfRawHexString reads one <..> hex string's raw bytes.
func pdfRawHexString(data []byte) ([]byte, int) {
	end := bytes.IndexByte(data, '>')
	if end < 0 {
		return nil, len(data)
	}
	decoded, err := pdfASCIIHex(data[1:end])
	if err != nil {
		return nil, end
	}

	return decoded, end
}

// pdfStringLiteral decodes one parenthesized literal through the byte
// decoder; pdfInfoTitle and the PostScript extractor read plain text here.
func pdfStringLiteral(data []byte) (string, int) {
	raw, consumed := pdfRawStringLiteral(data)

	return pdfDecodeStringBytes(raw), consumed
}

// pdfRawStringLiteral reads one parenthesized literal starting at
// data[0]=='(', resolving escapes, octal codes, and nested parentheses into
// the string's raw bytes and reporting the bytes consumed.
func pdfRawStringLiteral(data []byte) ([]byte, int) {
	raw := make([]byte, 0, 32)
	depth := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\\':
			escaped, consumed := pdfEscapedBytes(data[i+1:])
			raw = append(raw, escaped...)
			i += consumed
		case '(':
			depth++
			if depth > 1 {
				raw = append(raw, '(')
			}
		case ')':
			depth--
			if depth == 0 {
				return raw, i
			}
			raw = append(raw, ')')
		case '\r', '\n':
			raw = append(raw, ' ')
		default:
			raw = append(raw, data[i])
		}
	}

	return raw, len(data)
}

// pdfEscapedBytes resolves one backslash escape: octal codes become their
// byte (how TeX shows ligatures and accented glyphs), the whitespace escapes
// become a separator, a backslash before a line break continues the string,
// and anything else keeps the escaped byte itself.
func pdfEscapedBytes(rest []byte) ([]byte, int) {
	if len(rest) == 0 {
		return nil, 0
	}
	switch rest[0] {
	case '0', '1', '2', '3', '4', '5', '6', '7':
		value, end := 0, 0
		for ; end < 3 && end < len(rest) && rest[end] >= '0' && rest[end] <= '7'; end++ {
			value = value*8 + int(rest[end]-'0')
		}

		return []byte{byte(value)}, end
	case 'n', 'r', 't':
		return []byte{' '}, 1
	case 'b', 'f':
		return nil, 1
	case '\r':
		if len(rest) > 1 && rest[1] == '\n' {
			return nil, 2
		}

		return nil, 1
	case '\n':
		return nil, 1
	default:
		return rest[:1], 1
	}
}

// texLigatureNames spell out the ff/fi/fl ligature glyphs that TeX text fonts
// keep at 0x0B..0x0F (the OT1 built-in encoding) and 0x1B..0x1F (the T1/Cork
// encoding); both ranges are unused control bytes in every standard PDF
// encoding, so expanding them never corrupts non-TeX text and turns "\014ux"
// back into "flux" and "\034rmly" back into "firmly".
var texLigatureNames = [...]string{"ff", "fi", "fl", "ffi", "ffl"}

// pdfDecodeStringBytes turns one show-string's raw bytes into indexable text:
// UTF-16BE strings decode by their BOM, and single-byte strings keep printable
// ASCII, expand the TeX ligature codes, and read high bytes as Latin-1.
func pdfDecodeStringBytes(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		return pdfUTF16Text(raw[2:])
	}
	var out strings.Builder
	for _, b := range raw {
		switch {
		case printableASCII(b):
			out.WriteByte(b)
		case b >= 0x0B && b <= 0x0F:
			out.WriteString(texLigatureNames[b-0x0B])
		case b >= 0x1B && b <= 0x1F:
			out.WriteString(texLigatureNames[b-0x1B])
		case b >= 0xA0:
			out.WriteRune(rune(b))
		}
	}

	return out.String()
}

// pdfUTF16Text decodes UTF-16BE string bytes, keeping printable runes.
func pdfUTF16Text(raw []byte) string {
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		units = append(units, uint16(raw[i])<<8|uint16(raw[i+1]))
	}
	var out strings.Builder
	for _, r := range utf16.Decode(units) {
		if r >= 0x20 && r != 0x7F {
			out.WriteRune(r)
		} else {
			out.WriteByte(' ')
		}
	}

	return out.String()
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

// pdfInfoTitle reads the document-info /Title string when present — a
// parenthesized literal or a <hex> string (how UTF-16 titles are written).
func pdfInfoTitle(body []byte) string {
	index := bytes.Index(body, []byte("/Title"))
	if index < 0 {
		return ""
	}
	rest := body[index+len("/Title"):]
	open := bytes.IndexAny(rest[:min(len(rest), 9)], "(<")
	if open < 0 {
		return ""
	}
	title := ""
	if rest[open] == '(' {
		title, _ = pdfStringLiteral(rest[open:])
	} else {
		title, _ = pdfHexString(rest[open:])
	}

	return strings.TrimSpace(title)
}

// parsePostScript extracts the parenthesized text literals of a PostScript
// program — the operands of its show operators.
func parsePostScript(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	if !bytes.HasPrefix(body, []byte("%!")) {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	var out strings.Builder
	writeShownStrings(&out, body, nil, nil)
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
