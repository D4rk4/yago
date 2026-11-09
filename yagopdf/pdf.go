package yagopdf

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
	"unicode"
)

const (
	pdfMaxStreams              = 256
	pdfMaxStreamBytes          = 8 << 20
	pdfMaxDecodedDocumentBytes = 32 << 20
	pdfMaxTextBytes            = 1 << 20
	pdfMaxTitleBytes           = 4 << 10
)

type Document struct {
	Title string
	Text  string
}

func ReadPDF(body []byte) (Document, bool) {
	return pdfReadDocument(body, nil)
}

func pdfReadDocument(body []byte, abandoned <-chan struct{}) (Document, bool) {
	if !bytes.HasPrefix(body, []byte("%PDF")) {
		return Document{}, false
	}
	body, readable := pdfDecryptedBody(body)
	if !readable {
		return Document{}, false
	}
	quota := newPDFDecodeQuota(pdfMaxDecodedDocumentBytes)
	quota.abandoned = abandoned
	body = pdfWithUnpackedObjectStreams(body, quota)
	extracted := strings.TrimSpace(collapseBlankRuns(
		pdfCollapseHorizontalWhitespace(pdfPageText(body, quota, pdfMaxTextBytes)),
	))
	if !hasIndexableText(extracted) {
		return Document{}, false
	}

	return Document{Title: pdfInfoTitle(body), Text: extracted}, true
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
		return nil
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

		return filters
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

func pdfLZWWithin(raw []byte, limit int) ([]byte, error) {
	reader := lzw.NewReader(bytes.NewReader(raw), lzw.MSB, 8)
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, int64(max(0, limit))))
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("lzw: %w", err)
	}

	return data, nil
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

// hasIndexableText requires a minimum of real letters so CID-font garbage
// does not index.
func hasIndexableText(text string) bool {
	letters := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
		}
		if letters >= 8 {
			return true
		}
	}

	return false
}

// ReadPostScript extracts the parenthesized text literals of a PostScript
// program — the operands of its show operators. PostScript carries no
// document-info title.
func ReadPostScript(body []byte) (Document, bool) {
	return postScriptReadDocument(body, nil)
}

func postScriptReadDocument(body []byte, abandoned <-chan struct{}) (Document, bool) {
	if !bytes.HasPrefix(body, []byte("%!")) {
		return Document{}, false
	}
	out := newPDFTextCollector(pdfMaxTextBytes)
	out.abandoned = abandoned
	pdfWritePostScriptText(out, body)
	extracted := strings.TrimSpace(collapseBlankRuns(out.String()))
	if !hasIndexableText(extracted) {
		return Document{}, false
	}

	return Document{Text: extracted}, true
}
