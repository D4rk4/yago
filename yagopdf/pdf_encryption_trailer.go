package yagopdf

import "bytes"

const pdfMaxSecurityStringBytes = 256

func pdfEncryptionDictionary(entry []byte, lookup pdfObjectLookup) ([]byte, string) {
	reference := pdfLeadingReference(entry)
	if reference == "" {
		return pdfDirectDictionary(entry), ""
	}

	return pdfDirectDictionary(lookup.value(reference)), reference
}

func pdfFirstDocumentIdentifier(body []byte) []byte {
	entry, _ := pdfLastTrailerEntry(body, "ID")
	if !bytes.HasPrefix(entry, []byte("[")) {
		return nil
	}

	return pdfSecurityString(bytes.TrimLeft(entry[1:], "\t \r\n"))
}

func pdfLastTrailerEntry(body []byte, name string) ([]byte, bool) {
	return pdfTrailerEntry(pdfScanFileStructure(body).trailers, name)
}

func pdfTrailerEntry(trailers [][]byte, name string) ([]byte, bool) {
	for index := len(trailers) - 1; index >= 0; index-- {
		if value := pdfDictionaryEntryValue(trailers[index], name); value != nil {
			return value, true
		}
	}

	return nil, false
}

func pdfSecurityString(value []byte) []byte {
	switch {
	case bytes.HasPrefix(value, []byte("(")):
		raw, _ := pdfRawStringLiteralWithin(value, pdfMaxSecurityStringBytes)

		return raw
	case bytes.HasPrefix(value, []byte("<")):
		raw, _ := pdfRawHexStringWithin(value, pdfMaxSecurityStringBytes)

		return raw
	default:
		return nil
	}
}
