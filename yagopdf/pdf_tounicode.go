package yagopdf

import (
	"bytes"
	"regexp"
)

const (
	pdfMaxFontTables    = 64
	pdfMaxCMapEntries   = 8192
	pdfMaxCMapTextBytes = 8 << 20
	pdfMaxObjScanBytes  = 16 << 20
)

type pdfCMap struct {
	codeLen      int
	text         map[uint32]string
	ranges       []pdfCMapRange
	fallback     *pdfCMap
	omitUnmapped bool
	widths       *pdfGlyphWidths
}

var (
	pdfFontRefPattern = regexp.MustCompile(
		`/([^\x00\t\n\f\r ()<>\[\]{}/%]+)[\x00\t\n\f\r ]+(\d+)[\x00\t\n\f\r ]+(\d+)[\x00\t\n\f\r ]+R`,
	)
	pdfHexTokenPattern = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
)

func pdfObjectStreamWithQuota(
	objectValue func(string) []byte,
	object string,
	quota *pdfDecodeQuota,
) []byte {
	stream, exists := pdfEncodedStreamOf(objectValue(object))
	if !exists {
		return nil
	}
	decoded, ok := quota.decode(stream)
	if !ok {
		return nil
	}

	return decoded
}

func pdfParseCMapWithQuota(src []byte, quota *pdfCMapQuota) *pdfCMap {
	if src == nil {
		return nil
	}
	table := &pdfCMap{
		codeLen:      pdfCMapCodeLen(src),
		text:         map[uint32]string{},
		omitUnmapped: true,
	}
	for _, section := range pdfCMapSections(src, "bfchar") {
		tokens := pdfHexTokenPattern.FindAllSubmatch(section, quota.remainingEntries*2)
		for i := 0; i+1 < len(tokens) && len(table.text) < pdfMaxCMapEntries &&
			quota.remainingEntries > 0; i += 2 {
			code, ok := pdfHexValue(tokens[i][1])
			if !ok {
				quota.remainingEntries--

				continue
			}
			text, valid := pdfCMapUnicodeText(tokens[i+1][1])
			if !valid {
				quota.remainingEntries--

				continue
			}
			if !quota.put(table, code, text) {
				break
			}
		}
	}
	for _, section := range pdfCMapSections(src, "bfrange") {
		pdfParseBFRanges(table, section, quota)
	}
	table.ranges = pdfDisjointCodeSpans(table.ranges)
	if len(table.text) == 0 && len(table.ranges) == 0 {
		return nil
	}

	return table
}

func pdfParseBFRanges(
	table *pdfCMap,
	section []byte,
	quota *pdfCMapQuota,
) {
	for len(section) > 0 && quota.remainingEntries > 0 {
		var line []byte
		line, section, _ = bytes.Cut(section, []byte("\n"))
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
		base, valid := pdfCMapUnicodeText(tokens[2][1])
		if !valid {
			quota.remainingEntries--

			continue
		}
		if quota.reserve(base) {
			table.ranges = append(table.ranges, pdfCMapRange{low: low, high: high, text: base})
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
