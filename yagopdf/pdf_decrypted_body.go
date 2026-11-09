package yagopdf

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"slices"
)

func pdfDecryptedBody(body []byte) ([]byte, bool) {
	lookup := newPDFObjectLookup(body)
	entry, encrypted := pdfTrailerEntry(lookup.trailers, "Encrypt")
	if !encrypted {
		return body, true
	}
	dictionary, encryptReference := pdfEncryptionDictionary(entry, lookup)
	security, unlocked := pdfStandardSecurityUnlocked(dictionary, pdfFirstDocumentIdentifier(body))
	if !unlocked {
		return nil, false
	}

	return security.decryptedObjects(body, lookup.objects, encryptReference), true
}

func (s pdfStandardSecurity) decryptedObjects(
	body []byte,
	objects []pdfIndirectObject,
	encryptReference string,
) []byte {
	inFileOrder := slices.SortedFunc(
		slices.Values(objects),
		func(left, right pdfIndirectObject) int {
			return cmp.Compare(left.end, right.end)
		},
	)
	var decrypted bytes.Buffer
	decrypted.Grow(len(body))
	copied := 0
	for _, object := range inFileOrder {
		start := object.end - len("endobj") - len(object.value)
		_, _ = decrypted.Write(body[copied:start])
		if object.reference == encryptReference {
			_, _ = decrypted.Write(object.value)
		} else {
			_, _ = decrypted.Write(s.decryptedObject(object))
		}
		copied = start + len(object.value)
	}
	_, _ = decrypted.Write(body[copied:])

	return decrypted.Bytes()
}

func (s pdfStandardSecurity) decryptedObject(object pdfIndirectObject) []byte {
	stream, isStream := pdfEncodedStreamOf(object.value)
	if !isStream {
		return s.decryptedStrings(object.reference, object.value)
	}
	if s.streamInClear(stream.dictionary) {
		return object.value
	}
	dictionaryEnd := len(stream.dictionary)
	dataStart, _, dataEnd, _ := pdfStreamBoundaries(object.value)
	payload := s.decrypted(
		s.streamMethod,
		object.reference,
		pdfCipherStreamPayload(stream, s.streamMethod),
	)
	var decrypted bytes.Buffer
	dictionary := s.decryptedStrings(object.reference, stream.dictionary)
	_, _ = decrypted.Write(pdfDecryptedStreamDictionary(dictionary, len(payload)))
	_, _ = decrypted.Write(object.value[dictionaryEnd:dataStart])
	_, _ = decrypted.Write(payload)
	_, _ = decrypted.WriteString("\n")
	_, _ = decrypted.Write(object.value[dataEnd:])

	return decrypted.Bytes()
}

func (s pdfStandardSecurity) streamInClear(dictionary []byte) bool {
	streamType, _ := pdfDictionaryName(dictionary, "Type")

	return streamType == "XRef" || streamType == "Metadata" && !s.encryptMetadata
}

func (s pdfStandardSecurity) decryptedStrings(reference string, value []byte) []byte {
	if s.stringMethod == pdfCryptIdentity {
		return value
	}
	var decrypted bytes.Buffer
	for copied := 0; copied < len(value); {
		start, end, raw := pdfNextString(value, copied)
		_, _ = decrypted.Write(value[copied:start])
		if start < end {
			_, _ = decrypted.WriteString(
				"<" + hex.EncodeToString(s.decrypted(s.stringMethod, reference, raw)) + ">",
			)
		}
		copied = end
	}

	return decrypted.Bytes()
}

func pdfNextString(value []byte, at int) (int, int, []byte) {
	for at < len(value) {
		switch {
		case value[at] == '%':
			at = pdfLineEnd(value, at+1)
		case value[at] == '(':
			raw, end := pdfRawStringLiteralWithin(value[at:], len(value))

			return at, min(at+end+1, len(value)), raw
		case bytes.HasPrefix(value[at:], []byte("<<")):
			at += 2
		case value[at] == '<':
			raw, end := pdfRawHexStringWithin(value[at:], len(value))

			return at, min(at+end+1, len(value)), raw
		default:
			at++
		}
	}

	return len(value), len(value), nil
}
