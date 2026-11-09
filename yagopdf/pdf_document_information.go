package yagopdf

import (
	"bytes"
	"strings"
)

func pdfInfoTitle(body []byte) string {
	reference, referenced := pdfDocumentInformationReference(body)
	if !referenced {
		return ""
	}
	lookup := newPDFObjectLookup(body)
	entry := lookup.entry(lookup.value(reference), "Title")
	title := newPDFTextCollector(pdfMaxTitleBytes)
	switch {
	case bytes.HasPrefix(entry, []byte("(")):
		raw, _ := pdfRawStringLiteralWithin(entry, title.rawOperandLimit())
		pdfWriteDecodedStringBytes(title, raw)
	case bytes.HasPrefix(entry, []byte("<")) && !bytes.HasPrefix(entry, []byte("<<")):
		raw, _ := pdfRawHexStringWithin(entry, title.rawOperandLimit())
		pdfWriteDecodedStringBytes(title, raw)
	}

	return strings.Join(strings.Fields(title.String()), " ")
}

func pdfDocumentInformationReference(body []byte) (string, bool) {
	entry, exists := pdfLastTrailerEntry(body, "Info")

	return pdfLeadingReference(entry), exists
}
