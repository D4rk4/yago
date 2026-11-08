package yagopdf

import "bytes"

func pdfInfoTitle(body []byte) string {
	reference, referenced := pdfDocumentInformationReference(body)
	if !referenced {
		return pdfFirstTitleEntry(body)
	}

	information, found := newPDFObjectLookup(body).byReference[reference]
	if !found {
		return ""
	}

	return pdfFirstTitleEntry(information.value)
}

func pdfDocumentInformationReference(body []byte) (string, bool) {
	end := len(body)
	for {
		index := bytes.LastIndex(body[:end], []byte("/Info"))
		if index < 0 {
			return "", false
		}
		rest := bytes.TrimLeft(body[index+len("/Info"):], "\t \r\n")
		if match := pdfLeadingReferencePattern.FindSubmatch(rest); match != nil {
			return string(match[1]) + " " + string(match[2]), true
		}
		end = index
	}
}
