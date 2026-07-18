package formatparse

import "bytes"

const pdfMaxEncodingDictionaryBytes = 64 << 10

type pdfEncodingDifference struct {
	code uint32
	text string
}

func pdfSimpleFontEncodingTable(font []byte, lookup pdfObjectLookup) *pdfCMap {
	return pdfSimpleFontEncodingTableWithQuota(
		font,
		lookup,
		newPDFDecodeQuota(pdfMaxDecodedDocumentBytes),
	)
}

func pdfSimpleFontEncodingTableWithQuota(
	font []byte,
	lookup pdfObjectLookup,
	quota *pdfDecodeQuota,
) *pdfCMap {
	font = font[:min(len(font), pdfMaxEncodingDictionaryBytes)]
	if !quota.consume(len(font)) {
		return nil
	}
	definition := pdfDictionaryEntryValue(font, "Encoding")
	base, differences, exists := pdfFontEncodingDefinition(definition, lookup, quota)
	if !exists {
		return nil
	}
	table := pdfBaseEncodingTable(base)
	for _, difference := range differences {
		if difference.text == "" {
			delete(table, difference.code)

			continue
		}
		table[difference.code] = difference.text
	}

	return &pdfCMap{codeLen: 1, text: table, omitUnmapped: true}
}

func pdfFontEncodingDefinition(
	value []byte,
	lookup pdfObjectLookup,
	quota *pdfDecodeQuota,
) (string, []pdfEncodingDifference, bool) {
	value = bytes.TrimLeft(value, "\t \r\n")
	if len(value) == 0 {
		return "", nil, false
	}
	if name, _, ok := pdfDecodedNameToken(value, pdfMaxPDFNameBytes); ok {
		return name, nil, true
	}
	if reference := pdfLeadingReference(value); reference != "" {
		value = lookup.value(reference)
		value = value[:min(len(value), pdfMaxEncodingDictionaryBytes)]
		if !quota.consume(len(value)) {
			return "", nil, false
		}
	}
	dictionary := pdfDirectDictionary(value)
	if dictionary == nil {
		return "", nil, false
	}
	base, _ := pdfDictionaryName(dictionary, "BaseEncoding")
	differenceValue := pdfDictionaryEntryValue(dictionary, "Differences")
	if differenceValue == nil {
		return base, nil, base != ""
	}
	differences, valid := pdfEncodingDifferences(differenceValue)
	if !valid {
		return "", nil, false
	}

	return base, differences, true
}

func pdfDictionaryName(dictionary []byte, entry string) (string, bool) {
	name, _, exists := pdfDecodedNameToken(
		bytes.TrimLeft(pdfDictionaryEntryValue(dictionary, entry), "\t \r\n"),
		pdfMaxPDFNameBytes,
	)

	return name, exists
}

func pdfEncodingDifferences(value []byte) ([]pdfEncodingDifference, bool) {
	value = bytes.TrimLeft(value, "\t \r\n")
	if len(value) == 0 || value[0] != '[' {
		return nil, false
	}
	end := bytes.IndexByte(value[1:], ']')
	if end < 0 {
		return nil, false
	}
	value = value[1 : end+1]
	differences := make([]pdfEncodingDifference, 0, min(32, len(value)/3))
	nextCode := uint32(0)
	nextCodeAvailable := false
	for at := 0; at < len(value) && len(differences) < 256; {
		at = pdfSkipEncodingSpace(value, at)
		if at >= len(value) {
			break
		}
		if value[at] == '/' {
			difference, next, available, consumed, mapped := pdfEncodingDifferenceName(
				value[at:],
				nextCode,
				nextCodeAvailable,
			)
			at += max(1, consumed)
			nextCode = next
			nextCodeAvailable = available
			if mapped {
				differences = append(differences, difference)
			}

			continue
		}
		code, available, consumed := pdfNextEncodingCode(value[at:])
		if consumed > 0 {
			nextCode = code
			nextCodeAvailable = available
			at += consumed

			continue
		}
		nextCodeAvailable = false
		at++
	}

	return differences, true
}

func pdfEncodingDifferenceName(
	value []byte,
	nextCode uint32,
	nextCodeAvailable bool,
) (pdfEncodingDifference, uint32, bool, int, bool) {
	name, consumed, valid := pdfDecodedNameToken(value, pdfMaxGlyphNameBytes)
	if !nextCodeAvailable || nextCode > 255 || consumed <= 1 {
		nextCodeAvailable = nextCodeAvailable && valid && nextCode <= 255

		return pdfEncodingDifference{}, nextCode, nextCodeAvailable, consumed, false
	}
	text := ""
	if valid {
		text = pdfGlyphNameText(name)
	}

	return pdfEncodingDifference{code: nextCode, text: text}, nextCode + 1, true, consumed, true
}

func pdfNextEncodingCode(value []byte) (uint32, bool, int) {
	code, consumed, valid := pdfEncodingCode(value)

	return code, valid, consumed
}

func pdfSkipEncodingSpace(value []byte, at int) int {
	for at < len(value) {
		if pdfWhiteSpace(value[at]) {
			at++

			continue
		}
		if value[at] != '%' {
			break
		}
		at = pdfLineEnd(value, at+1)
	}

	return at
}

func pdfEncodingCode(value []byte) (uint32, int, bool) {
	end := 0
	for end < len(value) && !pdfWhiteSpace(value[end]) && !pdfNameDelimiter(value[end]) {
		end++
	}
	if end == 0 {
		return 0, 0, false
	}
	value = value[:end]
	at := 0
	negative := false
	if at < len(value) && (value[at] == '+' || value[at] == '-') {
		negative = value[at] == '-'
		at++
	}
	digitStart := at
	code := uint32(0)
	for at < len(value) && value[at] >= '0' && value[at] <= '9' {
		if code <= 256 {
			code = code*10 + uint32(value[at]-'0')
		}
		at++
	}

	return code, end, at == end && at > digitStart && !negative && code <= 255
}
