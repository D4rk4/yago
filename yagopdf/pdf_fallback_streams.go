package yagopdf

import "bytes"

func pdfFallbackDescriptionStreams(body []byte, objects []pdfIndirectObject) []pdfEncodedStream {
	if len(objects) == 0 {
		return pdfLooseDescriptionStreams(body)
	}
	excluded := pdfNonContentReferences(objects)
	streams := make([]pdfEncodedStream, 0, 8)
	for _, object := range objects {
		if len(streams) >= pdfMaxStreams {
			break
		}
		if _, skip := excluded[object.reference]; skip {
			continue
		}
		stream, ok := pdfEncodedStreamOf(object.value)
		if ok && !pdfExcludedStream(stream.dictionary) {
			streams = append(streams, stream)
		}
	}

	return streams
}

func pdfExcludedStream(dictionary []byte) bool {
	streamType, _, _ := pdfDecodedNameToken(
		pdfDictionaryEntryValue(dictionary, "Type"), pdfMaxPDFNameBytes,
	)
	switch streamType {
	case "Metadata", "ObjStm", "XRef", "EmbeddedFile", "CMap":
		return true
	}
	subtype, _, _ := pdfDecodedNameToken(
		pdfDictionaryEntryValue(dictionary, "Subtype"), pdfMaxPDFNameBytes,
	)
	if subtype == "Image" || subtype == "XML" {
		return true
	}

	return pdfDictionaryEntryValue(dictionary, "Length1") != nil ||
		pdfDictionaryEntryValue(dictionary, "Length2") != nil ||
		pdfDictionaryEntryValue(dictionary, "Length3") != nil
}

func pdfLooseDescriptionStreams(body []byte) []pdfEncodedStream {
	streams := make([]pdfEncodedStream, 0, 8)
	for at := 0; at < len(body) && len(streams) < pdfMaxStreams; {
		at = pdfSkipSpace(body, at)
		_, _, marker, exists := pdfStreamBoundaries(body[at:])
		if !exists {
			at = pdfValueEnd(body, at)

			continue
		}
		stream, _ := pdfEncodedStreamOf(body[at:])
		if !pdfExcludedStream(stream.dictionary) {
			streams = append(streams, stream)
		}
		at += marker + len("endstream")
	}

	return streams
}

func pdfNonContentReferences(objects []pdfIndirectObject) map[string]struct{} {
	excluded := make(map[string]struct{})
	byReference := make(map[string]pdfIndirectObject, len(objects))
	for _, object := range objects {
		byReference[object.reference] = object
		value := object.value
		if stream, exists := pdfEncodedStreamOf(value); exists {
			value = stream.dictionary
		}
		for _, reference := range pdfCollectNonContentReferences(value) {
			excluded[reference] = struct{}{}
		}
	}
	queue := make([]string, 0, len(excluded))
	for reference := range excluded {
		queue = append(queue, reference)
	}
	for len(queue) > 0 {
		reference := queue[0]
		queue = queue[1:]
		value := byReference[reference].value
		if _, stream := pdfEncodedStreamOf(value); !stream {
			queue = pdfAppendObjectReferences(queue, pdfNestedReferences(value), excluded)
		}
	}

	return excluded
}

func pdfCollectNonContentReferences(value []byte) []string {
	references := make([]string, 0, 8)
	for at := 0; at < len(value) && len(references) < pdfMaxIndirectObjects; {
		at = pdfSkipSpace(value, at)
		if at >= len(value) {
			break
		}
		if value[at] != '/' {
			at = pdfStructuralTokenEnd(value, at)

			continue
		}
		name, consumed, _ := pdfDecodedNameToken(value[at:], pdfMaxPDFNameBytes)
		at = pdfSkipSpace(value, at+consumed)
		switch name {
		case "FontFile", "FontFile2", "FontFile3", "ToUnicode", "Metadata", "JS", "EF", "XFA":
			end := pdfValueEnd(value, at)
			nested := pdfNestedReferences(value[at:end])
			remaining := pdfMaxIndirectObjects - len(references)
			references = append(references, nested[:min(remaining, len(nested))]...)
			at = end
		}
	}

	return references
}

func pdfNestedReferences(value []byte) []string {
	references := make([]string, 0, 8)
	for at := 0; at < len(value) && len(references) < pdfMaxIndirectObjects; {
		at = pdfSkipSpace(value, at)
		if reference, next := pdfReferenceAt(value, at); next > at {
			references = append(references, reference)
			at = next
		} else {
			at = pdfStructuralTokenEnd(value, at)
		}
	}

	return references
}

func pdfStructuralTokenEnd(value []byte, at int) int {
	if bytes.HasPrefix(value[at:], []byte("<<")) || bytes.HasPrefix(value[at:], []byte(">>")) {
		return at + 2
	}

	return pdfTokenEnd(value, at)
}
