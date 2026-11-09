package yagopdf

func pdfTextTablesWithQuota(body []byte, quota *pdfDecodeQuota) map[string]*pdfCMap {
	return newPDFFontResources(newPDFObjectLookup(body), quota).documentTables(body)
}

func (r *pdfFontResources) legacyFontNames(body []byte) ([]string, map[string]string) {
	objects := map[string]string{}
	names := make([]string, 0, pdfMaxFontTables)
	for _, reference := range pdfFontRefPattern.FindAllSubmatch(body, pdfMaxIndirectObjects) {
		name, _, valid := pdfDecodedNameToken(reference[0], pdfMaxPDFNameBytes)
		if !valid {
			continue
		}
		object := string(reference[2]) + " " + string(reference[3])
		if r.isFont(object) {
			names = pdfAppendFontName(names, objects, name, object)
		}
	}

	return names, objects
}

func (r *pdfFontResources) scopedFontNames(fonts []byte) ([]string, map[string]string) {
	objects := map[string]string{}
	names := make([]string, 0, pdfMaxFontTables)
	cursor := newPDFDictionaryCursor(fonts)
	for {
		name, value, exists := cursor.next()
		if !exists {
			return names, objects
		}
		object := pdfLeadingReference(value)
		if name != "" && object != "" && r.isFont(object) {
			names = pdfAppendFontName(names, objects, name, object)
		}
	}
}

func pdfAppendFontName(names []string, objects map[string]string, name, object string) []string {
	if previous, exists := objects[name]; exists {
		if previous != object {
			objects[name] = ""
		}

		return names
	}
	if len(names) < pdfMaxFontTables {
		objects[name] = object
		names = append(names, name)
	}

	return names
}

func pdfUnavailableFontTable() *pdfCMap {
	return &pdfCMap{codeLen: 1, text: map[uint32]string{}, omitUnmapped: true}
}
