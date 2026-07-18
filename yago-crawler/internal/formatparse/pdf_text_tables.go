package formatparse

func pdfTextTablesWithQuota(body []byte, quota *pdfDecodeQuota) map[string]*pdfCMap {
	if len(body) > pdfMaxObjScanBytes {
		body = body[:pdfMaxObjScanBytes]
	}
	lookup := newPDFObjectLookup(body)
	fontNames, objectOf := pdfFontResourceObjectReferences(body, lookup)
	primaryTables := pdfToUnicodeTablesFromObjects(fontNames, objectOf, lookup.value, quota)
	tables := make(map[string]*pdfCMap, min(len(fontNames), pdfMaxFontTables))
	for _, name := range fontNames[:min(len(fontNames), pdfMaxFontTables)] {
		object := objectOf[name]
		if object == "" {
			tables[name] = pdfUnavailableFontTable()

			continue
		}
		font := lookup.value(object + " 0")
		fallback := pdfSimpleFontEncodingTableWithQuota(font, lookup, quota)
		primary := primaryTables[name]
		switch {
		case primary != nil:
			tables[name] = &pdfCMap{
				codeLen:      primary.codeLen,
				text:         primary.text,
				fallback:     fallback,
				omitUnmapped: true,
			}
		case fallback != nil:
			tables[name] = fallback
		default:
			tables[name] = pdfUnavailableFontTable()
		}
	}

	return tables
}

func pdfFontResourceObjectReferences(
	body []byte,
	lookup pdfObjectLookup,
) ([]string, map[string]string) {
	objectOf := map[string]string{}
	fontNames := make([]string, 0, pdfMaxFontTables)
	for _, reference := range pdfFontRefPattern.FindAllSubmatch(body, pdfMaxIndirectObjects) {
		name, _, valid := pdfDecodedNameToken(reference[0], pdfMaxPDFNameBytes)
		if !valid {
			continue
		}
		object := string(reference[2])
		if !pdfFontDictionary(lookup.value(object + " 0")) {
			continue
		}
		if _, exists := objectOf[name]; !exists {
			if len(fontNames) >= pdfMaxFontTables {
				continue
			}
			fontNames = append(fontNames, name)
		}
		if seen, exists := objectOf[name]; exists && seen != object {
			objectOf[name] = ""

			continue
		}
		objectOf[name] = object
	}

	return fontNames, objectOf
}

func pdfFontDictionary(value []byte) bool {
	name, exists := pdfDictionaryName(value, "Type")

	return exists && name == "Font"
}

func pdfUnavailableFontTable() *pdfCMap {
	return &pdfCMap{codeLen: 1, text: map[uint32]string{}, omitUnmapped: true}
}
