package yagopdf

func pdfPageText(body []byte, quota *pdfDecodeQuota, textLimit int) string {
	lookup := newPDFObjectLookup(body)
	fonts := newPDFFontResources(lookup, quota)
	text := newPDFTextCollector(textLimit)
	text.abandoned = quota.abandoned
	var interpreter *pdfTextInterpreter
	var scope string
	for _, stream := range pdfPageDescriptionStreamsFromLookup(body, lookup) {
		if quota.exhausted() || text.complete() {
			break
		}
		if interpreter == nil || scope == "" || scope != stream.scope {
			tables := fonts.tables(stream.resources)
			if stream.scope == "" {
				tables = fonts.documentTables(body)
			}
			interpreter = newPDFTextInterpreter(text, tables)
			scope = stream.scope
		}
		decoded, ok := quota.decode(stream)
		if !ok || len(decoded) == 0 {
			continue
		}
		interpreter.read(decoded)
	}
	return text.String()
}
