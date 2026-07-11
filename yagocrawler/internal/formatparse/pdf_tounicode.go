package formatparse

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const (
	pdfMaxFontTables   = 64
	pdfMaxCMapEntries  = 8192
	pdfMaxObjScanBytes = 16 << 20
)

// pdfCMap is one font's parsed ToUnicode table: the text each show-string
// code stands for, keyed by the code read codeLen bytes at a time.
type pdfCMap struct {
	codeLen int
	text    map[uint32]string
}

var (
	pdfFontRefPattern = regexp.MustCompile(
		`/([A-Za-z][A-Za-z0-9_.]*)[\s\r\n]+(\d+)[\s\r\n]+0[\s\r\n]+R`,
	)
	pdfToUnicodeRefPattern = regexp.MustCompile(`/ToUnicode[\s\r\n]+(\d+)[\s\r\n]+0[\s\r\n]+R`)
	pdfHexTokenPattern     = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
)

// pdfToUnicodeTables maps every content-stream font name (the /F58 of an
// "/F58 11.9552 Tf") to its parsed ToUnicode CMap. Subset fonts move glyphs
// to arbitrary codes — modern pdfTeX shows "firmly" as "\002rmly" — so byte
// heuristics cannot recover their text; the CMap the writer embeds is the
// ground truth. A name bound to two different font objects is dropped rather
// than guessed at.
func pdfToUnicodeTables(body []byte) map[string]*pdfCMap {
	if len(body) > pdfMaxObjScanBytes {
		body = body[:pdfMaxObjScanBytes]
	}
	objectOf := map[string]string{}
	for _, ref := range pdfFontRefPattern.FindAllSubmatch(body, -1) {
		name, object := string(ref[1]), string(ref[2])
		if seen, ok := objectOf[name]; ok && seen != object {
			objectOf[name] = ""

			continue
		}
		objectOf[name] = object
	}
	tables := map[string]*pdfCMap{}
	cmaps := map[string]*pdfCMap{}
	for name, object := range objectOf {
		if object == "" || len(tables) >= pdfMaxFontTables {
			continue
		}
		cmapObject := pdfToUnicodeObjectOf(body, object)
		if cmapObject == "" {
			continue
		}
		if cached, ok := cmaps[cmapObject]; ok {
			tables[name] = cached

			continue
		}
		table := pdfParseCMap(pdfObjectStream(body, cmapObject))
		if table == nil {
			continue
		}
		cmaps[cmapObject] = table
		tables[name] = table
	}

	return tables
}

// pdfToUnicodeObjectOf reads the /ToUnicode reference out of one font
// object's dictionary; empty when the font carries none.
func pdfToUnicodeObjectOf(body []byte, object string) string {
	dict := pdfObjectBody(body, object)
	if dict == nil {
		return ""
	}
	if end := bytes.Index(dict, []byte("stream")); end >= 0 {
		dict = dict[:end]
	}
	ref := pdfToUnicodeRefPattern.FindSubmatch(dict)
	if ref == nil {
		return ""
	}

	return string(ref[1])
}

// pdfObjectBody returns the bytes of "N 0 obj .. endobj", nil when absent.
func pdfObjectBody(body []byte, object string) []byte {
	marker := []byte(fmt.Sprintf("%s 0 obj", object))
	at := 0
	for {
		index := bytes.Index(body[at:], marker)
		if index < 0 {
			return nil
		}
		start := at + index
		if start > 0 && body[start-1] >= '0' && body[start-1] <= '9' {
			at = start + len(marker)

			continue
		}
		rest := body[start+len(marker):]
		end := bytes.Index(rest, []byte("endobj"))
		if end < 0 {
			return rest
		}

		return rest[:end]
	}
}

// pdfObjectStream decodes the stream carried by object N through its own
// /Filter chain; nil when the object or its stream is missing or undecodable.
func pdfObjectStream(body []byte, object string) []byte {
	objectBody := pdfObjectBody(body, object)
	if objectBody == nil {
		return nil
	}
	index := bytes.Index(objectBody, []byte("stream"))
	if index < 0 {
		return nil
	}
	filters := pdfFilterChain(objectBody[:index])
	start := index + len("stream")
	if start < len(objectBody) && objectBody[start] == '\r' {
		start++
	}
	if start < len(objectBody) && objectBody[start] == '\n' {
		start++
	}
	end := bytes.Index(objectBody[start:], []byte("endstream"))
	if end < 0 {
		return nil
	}
	decoded, ok := pdfDecodeChain(objectBody[start:start+end], filters)
	if !ok {
		return nil
	}

	return decoded
}

// pdfParseCMap reads a ToUnicode CMap's bfchar and bfrange sections into a
// lookup table. The array form of bfrange is rare enough that its rows are
// skipped (their codes fall back to the byte decoder), and ranges are capped
// so a hostile CMap cannot balloon the table.
func pdfParseCMap(src []byte) *pdfCMap {
	if src == nil {
		return nil
	}
	table := &pdfCMap{codeLen: pdfCMapCodeLen(src), text: map[uint32]string{}}
	for _, section := range pdfCMapSections(src, "bfchar") {
		tokens := pdfHexTokenPattern.FindAllSubmatch(section, -1)
		for i := 0; i+1 < len(tokens) && len(table.text) < pdfMaxCMapEntries; i += 2 {
			code, ok := pdfHexValue(tokens[i][1])
			if !ok {
				continue
			}
			table.text[code] = pdfHexUTF16(tokens[i+1][1])
		}
	}
	for _, section := range pdfCMapSections(src, "bfrange") {
		pdfParseBFRanges(table, section)
	}
	if len(table.text) == 0 {
		return nil
	}

	return table
}

// pdfParseBFRanges folds one bfrange section's <lo> <hi> <dst> rows into the
// table, incrementing dst's last code point across the range.
func pdfParseBFRanges(table *pdfCMap, section []byte) {
	for _, line := range bytes.Split(section, []byte("\n")) {
		if bytes.ContainsRune(line, '[') {
			continue
		}
		tokens := pdfHexTokenPattern.FindAllSubmatch(line, 3)
		if len(tokens) != 3 {
			continue
		}
		low, okLow := pdfHexValue(tokens[0][1])
		high, okHigh := pdfHexValue(tokens[1][1])
		if !okLow || !okHigh || high < low {
			continue
		}
		base := []rune(pdfHexUTF16(tokens[2][1]))
		for code := low; code <= high && len(table.text) < pdfMaxCMapEntries; code++ {
			mapped := make([]rune, len(base))
			copy(mapped, base)
			delta := code - low
			mapped[len(mapped)-1] += rune(delta) //nolint:gosec // capped below rune width
			table.text[code] = string(mapped)
		}
	}
}

// pdfCMapCodeLen reads the codespace width in bytes, defaulting to one.
func pdfCMapCodeLen(src []byte) int {
	sections := pdfCMapSections(src, "codespacerange")
	if len(sections) == 0 {
		return 1
	}
	token := pdfHexTokenPattern.FindSubmatch(sections[0])
	if token == nil || len(token[1]) <= 2 {
		return 1
	}

	return 2
}

// pdfCMapSections returns the bodies between "begin<kind>" and "end<kind>".
func pdfCMapSections(src []byte, kind string) [][]byte {
	begin := []byte("begin" + kind)
	end := []byte("end" + kind)
	sections := make([][]byte, 0, 4)
	at := 0
	for {
		start := bytes.Index(src[at:], begin)
		if start < 0 {
			return sections
		}
		bodyStart := at + start + len(begin)
		stop := bytes.Index(src[bodyStart:], end)
		if stop < 0 {
			return sections
		}
		sections = append(sections, src[bodyStart:bodyStart+stop])
		at = bodyStart + stop + len(end)
	}
}

// pdfHexValue reads one <..> token's hex digits as a code value.
func pdfHexValue(digits []byte) (uint32, bool) {
	if len(digits) == 0 || len(digits) > 8 {
		return 0, false
	}
	value := uint32(0)
	for _, d := range digits {
		switch {
		case d >= '0' && d <= '9':
			value = value<<4 | uint32(d-'0')
		case d >= 'a' && d <= 'f':
			value = value<<4 | uint32(d-'a'+10)
		default:
			value = value<<4 | uint32(d-'A'+10)
		}
	}

	return value, true
}

// pdfHexUTF16 decodes a <..> destination token's UTF-16BE code points.
func pdfHexUTF16(digits []byte) string {
	compact := digits
	if len(compact)%2 == 1 {
		compact = append(append([]byte{}, compact...), '0')
	}
	raw := make([]byte, hex.DecodedLen(len(compact)))
	if _, err := hex.Decode(raw, compact); err != nil {
		return ""
	}

	return pdfUTF16Text(raw)
}

// pdfMapString renders one show-string through the font's ToUnicode table,
// reading codeLen bytes per glyph; unmapped codes fall back to the byte
// decoder so a sparse CMap still yields the ASCII around it.
func pdfMapString(raw []byte, cmap *pdfCMap) string {
	var out strings.Builder
	step := cmap.codeLen
	for at := 0; at < len(raw); at += step {
		if at+step > len(raw) {
			out.WriteString(pdfDecodeStringBytes(raw[at:]))

			break
		}
		code := uint32(raw[at])
		if step == 2 {
			code = code<<8 | uint32(raw[at+1]) //nolint:gosec // guarded above: at+step <= len(raw)
		}
		if text, ok := cmap.text[code]; ok {
			out.WriteString(text)

			continue
		}
		out.WriteString(pdfDecodeStringBytes(raw[at : at+step]))
	}

	return out.String()
}
