package yagopdf

const (
	pdfMaxGlyphWidthEntries     = 1 << 16
	pdfMaxGlyphWidthObjectBytes = 1 << 20
	pdfMaxSimpleFontCode        = 0xFF
	pdfMaxCharacterIdentifier   = 0xFFFF
	pdfDefaultCIDWidth          = 1000
)

func (r *pdfFontResources) glyphWidths(font []byte) *pdfGlyphWidths {
	subtype, _ := pdfDictionaryName(font, "Subtype")
	switch subtype {
	case "Type0":
		return r.compositeGlyphWidths(font)
	case "Type3":
		return nil
	default:
		return r.simpleGlyphWidths(font)
	}
}

func (r *pdfFontResources) simpleGlyphWidths(font []byte) *pdfGlyphWidths {
	firstNumber, numbered := pdfLeadingNumber(r.resolvedEntry(font, "FirstChar"))
	first, valid := pdfWholeCode(firstNumber, pdfMaxSimpleFontCode)
	widths := r.simpleWidthArray(r.resolvedEntry(font, "Widths"))
	if !numbered || !valid || len(widths) == 0 || len(widths) > 256-int(first) {
		return nil
	}
	span := pdfGlyphWidthSpan(first, widths)
	if last := r.resolvedEntry(font, "LastChar"); last != nil {
		number, valid := pdfLeadingNumber(last)
		if !valid || number != float64(span.high) {
			return nil
		}
	}
	descriptor := r.lookup.dictionary(r.resolvedEntry(font, "FontDescriptor"))
	missing, known := pdfLeadingNumber(r.resolvedEntry(descriptor, "MissingWidth"))

	return &pdfGlyphWidths{
		ranges:       []pdfGlyphWidthRange{span},
		missing:      missing,
		missingKnown: known,
	}
}

func (r *pdfFontResources) compositeGlyphWidths(font []byte) *pdfGlyphWidths {
	encoding, _ := pdfDictionaryName(font, "Encoding")
	descendant := r.descendantFont(font)
	if encoding != "Identity-H" || descendant == nil {
		return nil
	}
	defaultWidth := float64(pdfDefaultCIDWidth)
	if r.lookup.entry(descendant, "DW") != nil {
		var valid bool
		defaultWidth, valid = pdfLeadingNumber(r.resolvedEntry(descendant, "DW"))
		if !valid {
			return nil
		}
	}
	ranges := []pdfGlyphWidthRange(nil)
	if r.lookup.entry(descendant, "W") != nil {
		ranges = r.cidWidthArray(r.resolvedEntry(descendant, "W"))
		if ranges == nil {
			return nil
		}
	}

	return &pdfGlyphWidths{ranges: ranges, missing: defaultWidth, missingKnown: true}
}

func (r *pdfFontResources) descendantFont(font []byte) []byte {
	value := r.resolvedEntry(font, "DescendantFonts")
	if len(value) == 0 || value[0] != '[' {
		return nil
	}
	start := pdfSkipSpace(value, 1)
	end := pdfValueEnd(value, start)
	closing := pdfSkipSpace(value, end)
	if closing >= len(value) || value[closing] != ']' {
		return nil
	}
	element := value[start:end]
	if reference := pdfLeadingReference(element); reference != "" {
		element = r.chargedObject(reference)
	}

	return r.lookup.dictionary(element)
}

func (r *pdfFontResources) resolvedEntry(dictionary []byte, entry string) []byte {
	value := r.lookup.entry(dictionary, entry)
	if reference := pdfLeadingReference(value); reference != "" {
		return r.chargedObject(reference)
	}

	return value
}

func (r *pdfFontResources) chargedObject(reference string) []byte {
	if value, cached := r.widthObjects[reference]; cached {
		return value
	}
	value := r.lookup.value(reference)
	if len(value) > pdfMaxGlyphWidthObjectBytes || !r.quota.consume(len(value)) {
		value = nil
	}
	value = value[pdfSkipSpace(value, 0):]
	r.widthObjects[reference] = value

	return value
}
