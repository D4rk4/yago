package formatparse

import (
	"bytes"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type pdfDecodeQuota struct {
	remainingBytes int
}

type pdfCMapQuota struct {
	remainingEntries   int
	remainingTextBytes int
}

func newPDFDecodeQuota(limit int) *pdfDecodeQuota {
	return &pdfDecodeQuota{remainingBytes: max(0, limit)}
}

func newPDFCMapQuota(entries int, text int) *pdfCMapQuota {
	return &pdfCMapQuota{
		remainingEntries:   max(0, entries),
		remainingTextBytes: max(0, text),
	}
}

func (q *pdfCMapQuota) put(table *pdfCMap, code uint32, text string) bool {
	if q.remainingEntries == 0 || len(text) > q.remainingTextBytes {
		q.remainingEntries = 0
		q.remainingTextBytes = 0

		return false
	}
	table.text[code] = text
	q.remainingEntries--
	q.remainingTextBytes -= len(text)

	return true
}

func (q *pdfDecodeQuota) exhausted() bool {
	return q.remainingBytes == 0
}

func (q *pdfDecodeQuota) consume(work int) bool {
	if work > q.remainingBytes {
		q.remainingBytes = 0

		return false
	}
	q.remainingBytes -= work

	return true
}

func (q *pdfDecodeQuota) decode(stream pdfEncodedStream) ([]byte, bool) {
	decoded, decodedWorkBytes, ok := pdfDecodeChainWithin(
		stream.encoded,
		pdfFilterChain(stream.dictionary),
		q.remainingBytes,
	)
	q.remainingBytes -= min(q.remainingBytes, decodedWorkBytes)

	return decoded, ok
}

func pdfDecodeChainWithin(raw []byte, filters []string, quota int) ([]byte, int, bool) {
	quota = max(0, quota)
	if len(filters) == 0 {
		length := min(len(raw), min(pdfMaxStreamBytes, quota))

		return raw[:length], length, length > 0
	}
	data := raw
	decodedWorkBytes := 0
	for _, filter := range filters {
		remaining := quota - decodedWorkBytes
		if remaining <= 0 {
			return nil, decodedWorkBytes, false
		}
		var ok bool
		data, ok = pdfDecodeFilter(data, filter, min(pdfMaxStreamBytes, remaining))
		if !ok {
			return nil, decodedWorkBytes, false
		}
		decodedWorkBytes += len(data)
	}

	return data, decodedWorkBytes, true
}

type pdfTextCollector struct {
	text      strings.Builder
	remaining int
	sealed    bool
}

func newPDFTextCollector(limit int) *pdfTextCollector {
	return &pdfTextCollector{remaining: min(pdfMaxTextBytes, max(0, limit))}
}

func (c *pdfTextCollector) complete() bool {
	return c.sealed || c.remaining == 0
}

func (c *pdfTextCollector) rawOperandLimit() int {
	if c.complete() {
		return 0
	}

	return min(pdfMaxStreamBytes, c.remaining*2+2)
}

func (c *pdfTextCollector) writeByte(value byte) {
	if c.complete() {
		return
	}
	_ = c.text.WriteByte(value)
	c.remaining--
}

func (c *pdfTextCollector) writeRune(value rune) {
	if c.complete() {
		return
	}
	if value < 0 || value > utf8.MaxRune || value >= 0xD800 && value <= 0xDFFF {
		value = utf8.RuneError
	}
	width := utf8.RuneLen(value)
	if width > c.remaining {
		c.sealed = true

		return
	}
	_, _ = c.text.WriteRune(value)
	c.remaining -= width
}

func (c *pdfTextCollector) writeString(value string) {
	if c.complete() || value == "" {
		return
	}
	length := min(len(value), c.remaining)
	if length < len(value) {
		for length > 0 && !utf8.RuneStart(value[length]) {
			length--
		}
		c.sealed = true
	}
	_, _ = c.text.WriteString(value[:length])
	c.remaining -= length
}

func (c *pdfTextCollector) String() string {
	return c.text.String()
}

func pdfPageText(
	body []byte,
	tables map[string]*pdfCMap,
	quota *pdfDecodeQuota,
	textLimit int,
) string {
	text := newPDFTextCollector(textLimit)
	for _, stream := range pdfPageDescriptionStreams(body) {
		if quota.exhausted() || text.complete() {
			break
		}
		decoded, ok := quota.decode(stream)
		if !ok || len(decoded) == 0 {
			continue
		}
		pdfWriteContentText(text, decoded, tables)
	}

	return text.String()
}

func pdfWriteContentText(
	out *pdfTextCollector,
	content []byte,
	tables map[string]*pdfCMap,
) {
	var current *pdfCMap
	at := 0
	for !out.complete() {
		begin := bytes.Index(content[at:], []byte("BT"))
		if begin < 0 {
			return
		}
		blockStart := at + begin + 2
		end := bytes.Index(content[blockStart:], []byte("ET"))
		if end < 0 {
			return
		}
		current = pdfWriteShownStrings(
			out,
			content[blockStart:blockStart+end],
			tables,
			current,
		)
		out.writeByte('\n')
		at = blockStart + end + 2
	}
}

func pdfWriteShownStrings(
	out *pdfTextCollector,
	block []byte,
	tables map[string]*pdfCMap,
	current *pdfCMap,
) *pdfCMap {
	inArray := false
	for at := 0; at < len(block) && !out.complete(); at++ {
		switch block[at] {
		case '(':
			raw, consumed := pdfRawStringLiteralWithin(block[at:], out.rawOperandLimit())
			pdfWriteShownText(out, raw, current)
			at += consumed
			if !inArray {
				out.writeByte(' ')
			}
		case '<':
			if at+1 < len(block) && block[at+1] == '<' {
				at += pdfSkipDictionary(block[at:])

				continue
			}
			raw, consumed := pdfRawHexStringWithin(block[at:], out.rawOperandLimit())
			pdfWriteShownText(out, raw, current)
			at += consumed
			if !inArray {
				out.writeByte(' ')
			}
		case '[':
			inArray = true
		case ']':
			inArray = false
			out.writeByte(' ')
		case '-':
			if consumed, space := pdfArrayKernSpace(block[at:], inArray); space {
				out.writeByte(' ')
				at += consumed
			}
		case '/':
			selected, consumed, changed := pdfTextFontSelection(block[at:], tables, current)
			if changed {
				current = selected
				at += max(1, consumed) - 1

				continue
			}
			_, consumed, _ = pdfDecodedNameToken(block[at:], pdfMaxPDFNameBytes)
			at += max(1, consumed) - 1
		}
	}

	return current
}

func pdfRawStringLiteralWithin(data []byte, limit int) ([]byte, int) {
	raw := make([]byte, 0, min(32, max(0, limit)))
	depth := 0
	for index := 0; index < len(data); index++ {
		var value []byte
		switch data[index] {
		case '\\':
			var consumed int
			value, consumed = pdfEscapedBytes(data[index+1:])
			index += consumed
		case '(':
			depth++
			if depth > 1 {
				value = []byte{'('}
			}
		case ')':
			depth--
			if depth == 0 {
				return raw, index
			}
			value = []byte{')'}
		case '\r':
			value = []byte{'\n'}
			if index+1 < len(data) && data[index+1] == '\n' {
				index++
			}
		case '\n':
			value = []byte{'\n'}
		default:
			value = data[index : index+1]
		}
		if available := max(0, limit-len(raw)); available > 0 {
			raw = append(raw, value[:min(len(value), available)]...)
		}
	}

	return raw, len(data)
}

func pdfRawHexStringWithin(data []byte, limit int) ([]byte, int) {
	end := bytes.IndexByte(data, '>')
	if end < 0 {
		return nil, len(data)
	}
	decoded, err := pdfASCIIHexWithin(data[1:end], limit)
	if err != nil {
		return nil, end
	}

	return decoded, end
}

func pdfWriteShownText(out *pdfTextCollector, raw []byte, current *pdfCMap) {
	if current == nil {
		pdfWriteDecodedStringBytes(out, raw)

		return
	}
	step := current.codeLen
	for at := 0; at < len(raw) && !out.complete(); at += step {
		if at+step > len(raw) {
			if current.fallback != nil {
				pdfWriteShownText(out, raw[at:], current.fallback)
			} else if !current.omitUnmapped {
				pdfWriteDecodedStringBytes(out, raw[at:])
			}

			return
		}
		code := uint32(0)
		for _, value := range raw[at : at+step] {
			code = code<<8 | uint32(value)
		}
		if text, ok := current.text[code]; ok {
			out.writeString(text)

			continue
		}
		if current.fallback != nil {
			pdfWriteShownText(out, raw[at:at+step], current.fallback)

			continue
		}
		if !current.omitUnmapped {
			pdfWriteDecodedStringBytes(out, raw[at:at+step])
		}
	}
}

func pdfWriteDecodedStringBytes(out *pdfTextCollector, raw []byte) {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		pdfWriteUTF16Text(out, raw[2:])

		return
	}
	for _, value := range raw {
		switch {
		case printableASCII(value):
			out.writeByte(value)
		case value == '\t' || value == '\n' || value == '\r':
			out.writeByte(' ')
		case value >= 0xA0:
			out.writeRune(rune(value))
		}
		if out.complete() {
			return
		}
	}
}

func pdfWriteUTF16Text(out *pdfTextCollector, raw []byte) {
	for index := 0; index+1 < len(raw) && !out.complete(); {
		first := rune(uint16(raw[index])<<8 | uint16(raw[index+1]))
		index += 2
		value := first
		if first >= 0xD800 && first <= 0xDBFF && index+1 < len(raw) {
			second := rune(uint16(raw[index])<<8 | uint16(raw[index+1]))
			if second >= 0xDC00 && second <= 0xDFFF {
				value = utf16.DecodeRune(first, second)
				index += 2
			}
		}
		if value >= 0x20 && value != 0x7F {
			out.writeRune(value)
		} else {
			out.writeByte(' ')
		}
	}
}
