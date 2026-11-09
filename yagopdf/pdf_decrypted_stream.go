package yagopdf

import (
	"bytes"
	"strconv"
)

func pdfDecryptedStreamDictionary(dictionary []byte, length int) []byte {
	cursor := newPDFDictionaryCursor(dictionary)
	for {
		name, value, exists := cursor.next()
		if !exists {
			return dictionary
		}
		if name != "Length" {
			continue
		}
		var rewritten bytes.Buffer
		rewritten.Grow(len(dictionary) + 20)
		_, _ = rewritten.Write(dictionary[:cursor.at-len(value)])
		_, _ = rewritten.WriteString(strconv.Itoa(length))
		_, _ = rewritten.Write(dictionary[cursor.at:])

		return rewritten.Bytes()
	}
}
