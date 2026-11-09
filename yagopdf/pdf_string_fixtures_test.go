package yagopdf

func pdfASCIIHex(value []byte) ([]byte, error) {
	return pdfASCIIHexWithin(value, pdfMaxStreamBytes)
}

func pdfRawHexString(value []byte) ([]byte, int) {
	return pdfRawHexStringWithin(value, pdfMaxTextBytes)
}

func pdfRawStringLiteral(value []byte) ([]byte, int) {
	return pdfRawStringLiteralWithin(value, pdfMaxTextBytes)
}

func pdfDecodeStringBytes(value []byte) string {
	text := newPDFTextCollector(pdfMaxTextBytes)
	pdfWriteDecodedStringBytes(text, value)
	return text.String()
}
