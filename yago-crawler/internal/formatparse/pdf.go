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

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

const (
	pdfMaxStreams              = 256
	pdfMaxStreamBytes          = 8 << 20
	pdfMaxDecodedDocumentBytes = 32 << 20
	pdfMaxTextBytes            = 1 << 20
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
	quota := newPDFDecodeQuota(pdfMaxDecodedDocumentBytes)
	tables := pdfTextTablesWithQuota(body, quota)
	extracted := strings.TrimSpace(collapseBlankRuns(
		pdfCollapseHorizontalWhitespace(pdfPageText(body, tables, quota, pdfMaxTextBytes)),
	))
	if !hasIndexableText(extracted) {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	title := pdfInfoTitle(body)
	if title == "" {
		title = textTitle(extracted)
	}

	return pageparse.ParsedPage{URL: rawURL, Title: title, Text: extracted}, true
}

func pdfContentStreams(body []byte) [][]byte {
	return pdfContentStreamsWithQuota(
		body,
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
}

func pdfContentStreamsWithQuota(body []byte, quota *pdfDecodeQuota) [][]byte {
	streams := make([][]byte, 0, 8)
	for _, stream := range pdfPageDescriptionStreams(body) {
		decoded, ok := quota.decode(stream)
		if !ok || len(decoded) == 0 {
			if quota.exhausted() {
				break
			}

			continue
		}
		streams = append(streams, decoded)
		if quota.exhausted() {
			break
		}
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

func pdfDecodeFilter(raw []byte, filter string, limit int) ([]byte, bool) {
	var (
		decoded []byte
		err     error
	)
	switch filter {
	case "FlateDecode", "Fl":
		decoded, err = pdfInflateWithin(raw, limit)
	case "ASCII85Decode", "A85":
		decoded, err = pdfASCII85Within(raw, limit)
	case "ASCIIHexDecode", "AHx":
		decoded, err = pdfASCIIHexWithin(raw, limit)
	case "LZWDecode", "LZW":
		decoded, err = pdfLZWWithin(raw, limit)
	default:
		return nil, false
	}
	if err != nil {
		return nil, false
	}

	return decoded, true
}

func pdfInflate(raw []byte) ([]byte, error) {
	return pdfInflateWithin(raw, pdfMaxStreamBytes)
}

func pdfInflateWithin(raw []byte, limit int) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("zlib: %w", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, int64(max(0, limit))))
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("inflate: %w", err)
	}

	return data, nil
}

func pdfASCII85(raw []byte) ([]byte, error) {
	return pdfASCII85Within(raw, pdfMaxStreamBytes)
}

func pdfASCII85Within(raw []byte, limit int) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	raw = bytes.TrimSuffix(raw, []byte("~>"))
	decoder := ascii85.NewDecoder(bytes.NewReader(raw))
	data, err := io.ReadAll(io.LimitReader(decoder, int64(max(0, limit))))
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("ascii85: %w", err)
	}

	return data, nil
}

func pdfASCIIHex(raw []byte) ([]byte, error) {
	return pdfASCIIHexWithin(raw, pdfMaxStreamBytes)
}

func pdfASCIIHexWithin(raw []byte, limit int) ([]byte, error) {
	limit = max(0, limit)
	maximumDigits := limit * 2
	compact := make([]byte, 0, min(len(raw), maximumDigits))
	for _, char := range raw {
		switch char {
		case '>', ' ', '\n', '\r', '\t':
		default:
			if len(compact) < maximumDigits {
				compact = append(compact, char)
			}
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
	return pdfLZWWithin(raw, pdfMaxStreamBytes)
}

func pdfLZWWithin(raw []byte, limit int) ([]byte, error) {
	reader := lzw.NewReader(bytes.NewReader(raw), lzw.MSB, 8)
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, int64(max(0, limit))))
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("lzw: %w", err)
	}

	return data, nil
}

func pdfTextFromContent(content []byte, tables map[string]*pdfCMap) string {
	out := newPDFTextCollector(pdfMaxTextBytes)
	pdfWriteContentText(out, content, tables)

	return out.String()
}

func writeShownStrings(
	out *strings.Builder,
	block []byte,
	tables map[string]*pdfCMap,
	current *pdfCMap,
) *pdfCMap {
	bounded := newPDFTextCollector(max(0, pdfMaxTextBytes-out.Len()))
	current = pdfWriteShownStrings(bounded, block, tables, current)
	out.WriteString(bounded.String())

	return current
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
		case '\r':
			raw = append(raw, '\n')
			if i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
		case '\n':
			raw = append(raw, '\n')
		default:
			raw = append(raw, data[i])
		}
	}

	return raw, len(data)
}

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
	case 'n':
		return []byte{'\n'}, 1
	case 'r':
		return []byte{'\r'}, 1
	case 't':
		return []byte{'\t'}, 1
	case 'b':
		return []byte{'\b'}, 1
	case 'f':
		return []byte{'\f'}, 1
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

func pdfDecodeStringBytes(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		return pdfUTF16Text(raw[2:])
	}
	var out strings.Builder
	for _, b := range raw {
		switch {
		case printableASCII(b):
			out.WriteByte(b)
		case b == '\t' || b == '\n' || b == '\r':
			out.WriteByte(' ')
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
	title := newPDFTextCollector(pdfMaxTextBytes)
	if rest[open] == '(' {
		raw, _ := pdfRawStringLiteralWithin(rest[open:], title.rawOperandLimit())
		pdfWriteDecodedStringBytes(title, raw)
	} else {
		raw, _ := pdfRawHexStringWithin(rest[open:], title.rawOperandLimit())
		pdfWriteDecodedStringBytes(title, raw)
	}

	return strings.TrimSpace(title.String())
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
