package yagopdf

type pdfFontResources struct {
	lookup         pdfObjectLookup
	quota          *pdfDecodeQuota
	cmapQuota      *pdfCMapQuota
	widthBudget    *pdfGlyphWidthBudget
	fontObjects    map[string]bool
	fonts          map[string]*pdfCMap
	unicodeMaps    map[string]*pdfCMap
	resourceScopes map[pdfDictionaryIdentity]map[string]*pdfCMap
	fontScopes     map[pdfDictionaryIdentity]map[string]*pdfCMap
	widthArrays    map[pdfDictionaryIdentity][]float64
	cidWidthArrays map[pdfDictionaryIdentity][]pdfGlyphWidthRange
	widthObjects   map[string][]byte
	document       map[string]*pdfCMap
}

func newPDFFontResources(lookup pdfObjectLookup, quota *pdfDecodeQuota) *pdfFontResources {
	return &pdfFontResources{
		lookup:         lookup,
		quota:          quota,
		cmapQuota:      newPDFCMapQuota(pdfMaxCMapEntries, pdfMaxCMapTextBytes),
		widthBudget:    &pdfGlyphWidthBudget{remainingEntries: pdfMaxGlyphWidthEntries},
		fontObjects:    map[string]bool{},
		fonts:          map[string]*pdfCMap{},
		unicodeMaps:    map[string]*pdfCMap{},
		resourceScopes: map[pdfDictionaryIdentity]map[string]*pdfCMap{},
		fontScopes:     map[pdfDictionaryIdentity]map[string]*pdfCMap{},
		widthArrays:    map[pdfDictionaryIdentity][]float64{},
		cidWidthArrays: map[pdfDictionaryIdentity][]pdfGlyphWidthRange{},
		widthObjects:   map[string][]byte{},
	}
}

func (r *pdfFontResources) tables(resources []byte) map[string]*pdfCMap {
	key := pdfDictionaryIdentityOf(resources)
	if scope, cached := r.resourceScopes[key]; cached {
		return scope
	}
	scope := r.fontScope(pdfDictionaryForEntry(resources, "Font", r.lookup))
	r.resourceScopes[key] = scope

	return scope
}

func (r *pdfFontResources) documentTables(body []byte) map[string]*pdfCMap {
	if r.document == nil {
		r.document = r.namedTables(r.legacyFontNames(body[:min(len(body), pdfMaxObjScanBytes)]))
	}

	return r.document
}

func (r *pdfFontResources) fontScope(fonts []byte) map[string]*pdfCMap {
	key := pdfDictionaryIdentityOf(fonts)
	if scope, cached := r.fontScopes[key]; cached {
		return scope
	}
	scope := r.namedTables(r.scopedFontNames(fonts))
	r.fontScopes[key] = scope

	return scope
}

func (r *pdfFontResources) namedTables(
	fontNames []string,
	objectOf map[string]string,
) map[string]*pdfCMap {
	tables := make(map[string]*pdfCMap, len(fontNames))
	for _, name := range fontNames {
		object := objectOf[name]
		if object == "" {
			tables[name] = pdfUnavailableFontTable()

			continue
		}
		tables[name] = r.font(object)
	}

	return tables
}

func (r *pdfFontResources) isFont(object string) bool {
	if known, cached := r.fontObjects[object]; cached {
		return known
	}
	known := r.lookup.hasName(r.lookup.value(object), "Type", "Font")
	r.fontObjects[object] = known

	return known
}

func (r *pdfFontResources) font(object string) *pdfCMap {
	if table, cached := r.fonts[object]; cached {
		return table
	}
	if len(r.fonts) >= pdfMaxFontTables {
		return pdfUnavailableFontTable()
	}
	dictionary := r.lookup.value(object)
	table := r.textTable(dictionary)
	table.widths = r.glyphWidths(dictionary)
	if pdfIdentityEncodedFont(dictionary) {
		table.codeLen = 2
	}
	r.fonts[object] = table

	return table
}

func (r *pdfFontResources) textTable(dictionary []byte) *pdfCMap {
	primary := r.unicodeMap(dictionary)
	fallback := pdfSimpleFontEncodingTableWithQuota(dictionary, r.lookup, r.quota)
	switch {
	case primary != nil:
		return &pdfCMap{
			codeLen:      primary.codeLen,
			text:         primary.text,
			ranges:       primary.ranges,
			fallback:     fallback,
			omitUnmapped: true,
		}
	case fallback != nil:
		return fallback
	default:
		return pdfUnavailableFontTable()
	}
}

func (r *pdfFontResources) unicodeMap(dictionary []byte) *pdfCMap {
	object := pdfLeadingReference(r.lookup.entry(dictionary, "ToUnicode"))
	if object == "" {
		return nil
	}
	if table, cached := r.unicodeMaps[object]; cached {
		return table
	}
	table := pdfParseCMapWithQuota(
		pdfObjectStreamWithQuota(r.lookup.value, object, r.quota),
		r.cmapQuota,
	)
	r.unicodeMaps[object] = table

	return table
}

func pdfIdentityEncodedFont(dictionary []byte) bool {
	encoding, named := pdfDictionaryName(dictionary, "Encoding")

	return named && (encoding == "Identity-H" || encoding == "Identity-V")
}
