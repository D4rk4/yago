package yagopdf

import (
	"bytes"
	"strconv"
)

const pdfMaxIndirectObjects = 4096

type pdfIndirectObject struct {
	reference string
	value     []byte
	end       int
}

type pdfFileStructure struct {
	objects  []pdfIndirectObject
	trailers [][]byte
}

func pdfScanIndirectObjects(body []byte) []pdfIndirectObject {
	return pdfScanFileStructure(body).objects
}

func pdfScanFileStructure(body []byte) pdfFileStructure {
	body = body[:min(len(body), pdfMaxObjScanBytes)]
	structure := pdfFileStructure{objects: make([]pdfIndirectObject, 0, 64)}
	positions := make(map[string]int, 64)
	for at := 0; at < len(body) && len(structure.objects) < pdfMaxIndirectObjects; {
		at = pdfSkipSpace(body, at)
		if at >= len(body) {
			break
		}
		if pdfKeywordAt(body, at, "trailer") {
			at += len("trailer")
			if dictionary := pdfDirectDictionary(body[at:]); dictionary != nil {
				structure.trailers = append(structure.trailers, dictionary)
			}

			continue
		}
		reference, start := pdfIndirectObjectHeader(body, at)
		if reference == "" {
			at = pdfValueEnd(body, at)

			continue
		}
		end := pdfIndirectObjectEnd(body[start:])
		if end < 0 {
			break
		}
		at = start + end + len("endobj")
		object := pdfIndirectObject{reference: reference, value: body[start : start+end], end: at}
		if position, exists := positions[reference]; exists {
			structure.objects[position] = object
		} else {
			positions[reference] = len(structure.objects)
			structure.objects = append(structure.objects, object)
		}
		if dictionary := pdfCrossReferenceDictionary(object.value); dictionary != nil {
			structure.trailers = append(structure.trailers, dictionary)
		}
	}

	return structure
}

func pdfCrossReferenceDictionary(value []byte) []byte {
	stream, exists := pdfEncodedStreamOf(value)
	if !exists {
		return nil
	}
	name, _, _ := pdfDecodedNameToken(
		pdfDictionaryEntryValue(stream.dictionary, "Type"), pdfMaxPDFNameBytes,
	)
	if name != "XRef" {
		return nil
	}

	return stream.dictionary
}

func pdfIndirectObjectHeader(value []byte, at int) (string, int) {
	_, firstEnd, first := pdfObjectStreamInteger(value, at)
	if !first || firstEnd >= len(value) || !pdfWhiteSpace(value[firstEnd]) {
		return "", at
	}
	secondStart := pdfSkipSpace(value, firstEnd)
	_, secondEnd, second := pdfObjectStreamInteger(value, secondStart)
	if !second || secondEnd >= len(value) || !pdfWhiteSpace(value[secondEnd]) {
		return "", at
	}
	end := pdfSkipSpace(value, secondEnd)
	if !pdfKeywordAt(value, end, "obj") {
		return "", at
	}

	return string(value[at:firstEnd]) + " " + string(value[secondStart:secondEnd]), end + len("obj")
}

func pdfIndirectObjectEnd(value []byte) int {
	at := 0
	if _, _, marker, exists := pdfStreamBoundaries(value); exists {
		at = marker + len("endstream")
	} else if _, _, exists := pdfStreamHeader(value); exists {
		return -1
	}
	for at < len(value) {
		at = pdfSkipSpace(value, at)
		if pdfKeywordAt(value, at, "endobj") {
			return at
		}
		at = pdfValueEnd(value, at)
	}

	return -1
}

func pdfStreamHeader(value []byte) (int, int, bool) {
	at := pdfSkipSpace(value, 0)
	if bytes.HasPrefix(value[at:], []byte("<<")) {
		length := pdfDictionaryLength(value[at:])
		if length == 0 {
			return 0, 0, false
		}
		at = pdfSkipSpace(value, at+length)
	}
	if !pdfKeywordAt(value, at, "stream") {
		return 0, 0, false
	}
	start := at + len("stream")
	if start < len(value) && value[start] == '\r' {
		start++
	}
	if start < len(value) && value[start] == '\n' {
		start++
	}

	return at, start, start > at+len("stream")
}

func pdfStreamBoundaries(value []byte) (int, int, int, bool) {
	dictionaryEnd, start, exists := pdfStreamHeader(value)
	if !exists {
		return 0, 0, 0, false
	}
	lengthEntry := pdfDictionaryEntryValue(value[:dictionaryEnd], "Length")
	length, lengthError := strconv.Atoi(string(lengthEntry))
	if lengthError == nil && length >= 0 && length <= len(value)-start {
		end := start + length
		marker := pdfSkipSpace(value, end)
		if pdfKeywordAt(value, marker, "endstream") {
			return start, end, marker, true
		}
	}
	for at := start; at < len(value); {
		index := bytes.Index(value[at:], []byte("endstream"))
		if index < 0 {
			break
		}
		at += index
		if pdfKeywordAt(value, at, "endstream") {
			return start, at, at, true
		}
		at += len("endstream")
	}

	return 0, 0, 0, false
}

func pdfEncodedStreamOf(value []byte) (pdfEncodedStream, bool) {
	start, end, _, exists := pdfStreamBoundaries(value)
	if !exists {
		return pdfEncodedStream{}, false
	}
	dictionaryEnd, _, _ := pdfStreamHeader(value)

	return pdfEncodedStream{dictionary: value[:dictionaryEnd], encoded: value[start:end]}, true
}
