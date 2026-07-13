package formatparse

import (
	"bytes"
	"regexp"
)

const pdfMaxIndirectObjects = 4096

type pdfIndirectObject struct {
	reference string
	value     []byte
}

type pdfEncodedStream struct {
	dictionary []byte
	encoded    []byte
}

type pdfObjectLookup struct {
	objects     []pdfIndirectObject
	byReference map[string]pdfIndirectObject
}

var (
	pdfIndirectObjectHeaderPattern = regexp.MustCompile(
		`(?:^|[\r\n])[\t ]*(\d+)[\t ]+(\d+)[\t ]+obj`,
	)
	pdfStreamKeywordPattern     = regexp.MustCompile(`(?:^|[\t \r\n>])(stream)(?:\r\n|\r|\n)`)
	pdfIndirectReferencePattern = regexp.MustCompile(`(\d+)[\t \r\n]+(\d+)[\t \r\n]+R`)
	pdfLeadingReferencePattern  = regexp.MustCompile(`^(\d+)[\t \r\n]+(\d+)[\t \r\n]+R`)
	pdfPageTypePattern          = regexp.MustCompile(`/Type[\t \r\n]*/Page(?:[\t \r\n/<>()\[\]]|$)`)
	pdfFormSubtypePattern       = regexp.MustCompile(
		`/Subtype[\t \r\n]*/Form(?:[\t \r\n/<>()\[\]]|$)`,
	)
	pdfContentsPattern       = regexp.MustCompile(`/Contents[\t \r\n]*`)
	pdfExcludedStreamPattern = regexp.MustCompile(
		`/(?:Type[\t \r\n]*/(?:Metadata|ObjStm|XRef|EmbeddedFile|CMap)|Subtype[\t \r\n]*/(?:Image|XML)|Length[123][\t \r\n])`,
	)
	pdfExcludedStreamReferencePattern = regexp.MustCompile(
		`/(?:FontFile[23]?|ToUnicode|Metadata)[\t \r\n]+(\d+)[\t \r\n]+(\d+)[\t \r\n]+R`,
	)
)

func pdfPageDescriptionStreams(body []byte) []pdfEncodedStream {
	lookup := newPDFObjectLookup(body)
	objects := lookup.objects
	pageFound := false
	pageReferences := make([]string, 0, 16)
	pageResources := make([][]byte, 0, 16)
	for _, object := range objects {
		dictionary := object.value
		if stream, exists := pdfEncodedStreamOf(object.value); exists {
			dictionary = stream.dictionary
		}
		if !pdfPageTypePattern.Match(dictionary) {
			continue
		}
		pageFound = true
		pageReferences = pdfAppendReferences(
			pageReferences,
			pdfPageContentsReferences(dictionary),
			pdfMaxIndirectObjects,
		)
		if resources := pdfPageResourceDictionary(dictionary, lookup); resources != nil {
			pageResources = append(pageResources, resources)
		}
	}
	selectedReferences := pdfResolvedPageContentReferences(pageReferences, lookup.byReference)
	if pageFound {
		selectedReferences = pdfAppendUniqueReferences(
			selectedReferences,
			pdfReachableFormReferences(pageResources, lookup),
			pdfMaxIndirectObjects,
		)

		return pdfSelectedObjectStreams(lookup, selectedReferences)
	}

	return pdfFallbackDescriptionStreams(body, objects)
}

func pdfIndirectObjects(body []byte) []pdfIndirectObject {
	return newPDFObjectLookup(body).objects
}

func newPDFObjectLookup(body []byte) pdfObjectLookup {
	objects := pdfScanIndirectObjects(body)
	byReference := make(map[string]pdfIndirectObject, len(objects))
	for _, object := range objects {
		byReference[object.reference] = object
	}

	return pdfObjectLookup{objects: objects, byReference: byReference}
}

func (l pdfObjectLookup) value(reference string) []byte {
	object, exists := l.byReference[reference]
	if !exists {
		return nil
	}

	return object.value
}

func pdfScanIndirectObjects(body []byte) []pdfIndirectObject {
	if len(body) > pdfMaxObjScanBytes {
		body = body[:pdfMaxObjScanBytes]
	}
	objects := make([]pdfIndirectObject, 0, 64)
	objectPosition := make(map[string]int, 64)
	at := 0
	for len(objects) < pdfMaxIndirectObjects {
		header := pdfIndirectObjectHeaderPattern.FindSubmatchIndex(body[at:])
		if header == nil {
			break
		}
		valueStart := at + header[1]
		valueEnd := pdfIndirectObjectEnd(body[valueStart:])
		if valueEnd < 0 {
			break
		}
		reference := string(body[at+header[2]:at+header[3]]) + " " +
			string(body[at+header[4]:at+header[5]])
		object := pdfIndirectObject{
			reference: reference,
			value:     body[valueStart : valueStart+valueEnd],
		}
		if position, exists := objectPosition[reference]; exists {
			objects[position] = object
		} else {
			objectPosition[reference] = len(objects)
			objects = append(objects, object)
		}
		at = valueStart + valueEnd + len("endobj")
	}

	return objects
}

func pdfIndirectObjectEnd(value []byte) int {
	end := bytes.Index(value, []byte("endobj"))
	if end < 0 {
		return -1
	}
	stream := pdfStreamKeywordPattern.FindSubmatchIndex(value[:end])
	if stream == nil {
		return end
	}
	streamEnd := bytes.Index(value[stream[1]:], []byte("endstream"))
	if streamEnd < 0 {
		return -1
	}
	streamEnd += stream[1] + len("endstream")
	remainingEnd := bytes.Index(value[streamEnd:], []byte("endobj"))
	if remainingEnd < 0 {
		return -1
	}

	return streamEnd + remainingEnd
}

func pdfPageContentsReferences(value []byte) []string {
	entry := pdfContentsPattern.FindIndex(value)
	if entry == nil {
		return nil
	}
	rest := bytes.TrimLeft(value[entry[1]:], "\t \r\n")
	if len(rest) == 0 {
		return nil
	}
	if rest[0] == '[' {
		end := bytes.IndexByte(rest, ']')
		if end < 0 {
			return nil
		}

		return pdfReferences(rest[1:end])
	}
	match := pdfLeadingReferencePattern.FindSubmatch(rest)
	if match == nil {
		return nil
	}

	return []string{string(match[1]) + " " + string(match[2])}
}

func pdfReferences(value []byte) []string {
	matches := pdfIndirectReferencePattern.FindAllSubmatch(value, pdfMaxStreams)
	references := make([]string, 0, len(matches))
	for _, match := range matches {
		references = append(references, string(match[1])+" "+string(match[2]))
	}

	return references
}

func pdfAppendReferences(current []string, additional []string, limit int) []string {
	remaining := max(0, limit-len(current))

	return append(current, additional[:min(len(additional), remaining)]...)
}

func pdfAppendUniqueReferences(current []string, additional []string, limit int) []string {
	seen := make(map[string]struct{}, len(current)+len(additional))
	for _, reference := range current {
		seen[reference] = struct{}{}
	}
	for _, reference := range additional {
		if len(current) >= limit {
			break
		}
		if _, exists := seen[reference]; exists {
			continue
		}
		seen[reference] = struct{}{}
		current = append(current, reference)
	}

	return current
}

func pdfResolvedPageContentReferences(
	references []string,
	objectByReference map[string]pdfIndirectObject,
) []string {
	resolved := make([]string, 0, len(references))
	queued := make(map[string]struct{}, min(len(references), pdfMaxIndirectObjects))
	queue := pdfAppendObjectReferences(nil, references, queued)
	for len(queue) > 0 {
		reference := queue[0]
		queue = queue[1:]
		object, exists := objectByReference[reference]
		if !exists {
			continue
		}
		if _, stream := pdfEncodedStreamOf(object.value); stream {
			resolved = append(resolved, reference)

			continue
		}
		array := bytes.TrimSpace(object.value)
		if len(array) == 0 || array[0] != '[' {
			continue
		}
		if end := bytes.IndexByte(array, ']'); end >= 0 {
			queue = pdfAppendObjectReferences(queue, pdfReferences(array[1:end]), queued)
		}
	}

	return resolved
}

func pdfSelectedObjectStreams(
	lookup pdfObjectLookup,
	references []string,
) []pdfEncodedStream {
	streams := make([]pdfEncodedStream, 0, min(len(references), pdfMaxStreams))
	for _, reference := range references {
		if len(streams) >= pdfMaxStreams {
			break
		}
		if stream, ok := pdfEncodedStreamOf(lookup.value(reference)); ok {
			streams = append(streams, stream)
		}
	}

	return streams
}

func pdfFallbackDescriptionStreams(body []byte, objects []pdfIndirectObject) []pdfEncodedStream {
	excluded := make(map[string]struct{})
	for _, match := range pdfExcludedStreamReferencePattern.FindAllSubmatch(body, pdfMaxIndirectObjects) {
		excluded[string(match[1])+" "+string(match[2])] = struct{}{}
	}
	streams := make([]pdfEncodedStream, 0, 8)
	for _, object := range objects {
		if len(streams) >= pdfMaxStreams {
			break
		}
		if _, skip := excluded[object.reference]; skip {
			continue
		}
		stream, ok := pdfEncodedStreamOf(object.value)
		if !ok || pdfExcludedStreamPattern.Match(stream.dictionary) {
			continue
		}
		streams = append(streams, stream)
	}
	if len(objects) > 0 {
		return streams
	}

	return pdfLooseDescriptionStreams(body)
}

func pdfLooseDescriptionStreams(body []byte) []pdfEncodedStream {
	streams := make([]pdfEncodedStream, 0, 8)
	at := 0
	for len(streams) < pdfMaxStreams {
		index := bytes.Index(body[at:], []byte("stream"))
		if index < 0 {
			break
		}
		dictionaryStart := bytes.LastIndex(body[at:at+index], []byte("<<"))
		dictionary := []byte(nil)
		if dictionaryStart >= 0 {
			dictionary = body[at+dictionaryStart : at+index]
		}
		encodedStart := at + index + len("stream")
		if encodedStart < len(body) && body[encodedStart] == '\r' {
			encodedStart++
		}
		if encodedStart < len(body) && body[encodedStart] == '\n' {
			encodedStart++
		}
		encodedEnd := bytes.Index(body[encodedStart:], []byte("endstream"))
		if encodedEnd < 0 {
			break
		}
		at = encodedStart + encodedEnd + len("endstream")
		if pdfExcludedStreamPattern.Match(dictionary) {
			continue
		}
		streams = append(streams, pdfEncodedStream{
			dictionary: dictionary,
			encoded:    body[encodedStart : encodedStart+encodedEnd],
		})
	}

	return streams
}

func pdfEncodedStreamOf(value []byte) (pdfEncodedStream, bool) {
	stream := pdfStreamKeywordPattern.FindSubmatchIndex(value)
	if stream == nil {
		return pdfEncodedStream{}, false
	}
	streamStart := stream[2]
	encodedStart := stream[1]
	encodedEnd := bytes.Index(value[encodedStart:], []byte("endstream"))
	if encodedEnd < 0 {
		return pdfEncodedStream{}, false
	}

	return pdfEncodedStream{
		dictionary: value[:streamStart],
		encoded:    value[encodedStart : encodedStart+encodedEnd],
	}, true
}
