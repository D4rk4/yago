package formatparse

import (
	"bytes"
	"regexp"
	"strings"
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
	fallback     *pdfCMap
	omitUnmapped bool
}

var (
	pdfFontRefPattern = regexp.MustCompile(
		`/([^\x00\t\n\f\r ()<>\[\]{}/%]+)[\x00\t\n\f\r ]+(\d+)[\x00\t\n\f\r ]+0[\x00\t\n\f\r ]+R`,
	)
	pdfToUnicodeRefPattern = regexp.MustCompile(`/ToUnicode[\s\r\n]+(\d+)[\s\r\n]+0[\s\r\n]+R`)
	pdfHexTokenPattern     = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
)

func pdfToUnicodeTables(body []byte) map[string]*pdfCMap {
	return pdfToUnicodeTablesWithQuota(body, newPDFDecodeQuota(pdfMaxDecodedDocumentBytes))
}

func pdfToUnicodeTablesWithQuota(
	body []byte,
	quota *pdfDecodeQuota,
) map[string]*pdfCMap {
	if len(body) > pdfMaxObjScanBytes {
		body = body[:pdfMaxObjScanBytes]
	}
	lookup := newPDFObjectLookup(body)
	fontNames, objectOf := pdfFontObjectReferences(body)

	return pdfToUnicodeTablesFromObjects(fontNames, objectOf, lookup.value, quota)
}

func pdfFontObjectReferences(body []byte) ([]string, map[string]string) {
	objectOf := map[string]string{}
	fontNames := make([]string, 0, pdfMaxFontTables)
	for _, ref := range pdfFontRefPattern.FindAllSubmatch(body, pdfMaxIndirectObjects) {
		name, _, valid := pdfDecodedNameToken(ref[0], pdfMaxPDFNameBytes)
		if !valid {
			continue
		}
		object := string(ref[2])
		if _, exists := objectOf[name]; !exists {
			fontNames = append(fontNames, name)
		}
		if seen, ok := objectOf[name]; ok && seen != object {
			objectOf[name] = ""

			continue
		}
		objectOf[name] = object
	}

	return fontNames, objectOf
}

func pdfToUnicodeTablesFromObjects(
	fontNames []string,
	objectOf map[string]string,
	objectValue func(string) []byte,
	quota *pdfDecodeQuota,
) map[string]*pdfCMap {
	tables := map[string]*pdfCMap{}
	cmaps := map[string]*pdfCMap{}
	cmapQuota := newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes)
	for _, name := range fontNames {
		object := objectOf[name]
		if object == "" || len(tables) >= pdfMaxFontTables {
			continue
		}
		cmapObject := pdfToUnicodeObjectOf(objectValue, object)
		if cmapObject == "" {
			continue
		}
		if cached, ok := cmaps[cmapObject]; ok {
			tables[name] = cached

			continue
		}
		table := pdfParseCMapWithQuota(
			pdfObjectStreamWithQuota(objectValue, cmapObject, quota),
			cmapQuota,
		)
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
func pdfToUnicodeObjectOf(objectValue func(string) []byte, object string) string {
	dict := objectValue(object + " 0")
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

// pdfObjectStream decodes the stream carried by object N through its own
// /Filter chain; nil when the object or its stream is missing or undecodable.
func pdfObjectStream(lookup pdfObjectLookup, object string) []byte {
	return pdfObjectStreamWithQuota(
		lookup.value,
		object,
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
}

func pdfObjectStreamWithQuota(
	objectValue func(string) []byte,
	object string,
	quota *pdfDecodeQuota,
) []byte {
	objectBody := objectValue(object + " 0")
	if objectBody == nil {
		return nil
	}
	index := bytes.Index(objectBody, []byte("stream"))
	if index < 0 {
		return nil
	}
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
	decoded, ok := quota.decode(pdfEncodedStream{
		dictionary: objectBody[:index],
		encoded:    objectBody[start : start+end],
	})
	if !ok {
		return nil
	}

	return decoded
}

func pdfParseCMap(src []byte) *pdfCMap {
	return pdfParseCMapWithQuota(
		src,
		newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes),
	)
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
	if len(table.text) == 0 {
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
		for code := low; code <= high && len(table.text) < pdfMaxCMapEntries &&
			quota.remainingEntries > 0; code++ {
			delta := code - low
			mapped, usable := pdfIncrementedCMapText(base, delta)
			if !usable {
				quota.remainingEntries--

				continue
			}
			if !quota.put(table, code, mapped) {
				break
			}
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

func pdfMapString(raw []byte, cmap *pdfCMap) string {
	var out strings.Builder
	step := cmap.codeLen
	for at := 0; at < len(raw); at += step {
		if at+step > len(raw) {
			if cmap.fallback != nil {
				out.WriteString(pdfMapString(raw[at:], cmap.fallback))
			} else if !cmap.omitUnmapped {
				out.WriteString(pdfDecodeStringBytes(raw[at:]))
			}

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
		if cmap.fallback != nil {
			out.WriteString(pdfMapString(raw[at:at+step], cmap.fallback))

			continue
		}
		if !cmap.omitUnmapped {
			out.WriteString(pdfDecodeStringBytes(raw[at : at+step]))
		}
	}

	return out.String()
}
