package yagopdf

type pdfEncodedStream struct {
	dictionary []byte
	encoded    []byte
	resources  []byte
	scope      string
}

func pdfPageDescriptionStreams(body []byte) []pdfEncodedStream {
	return pdfPageDescriptionStreamsFromLookup(body, newPDFObjectLookup(body))
}

func pdfPageDescriptionStreamsFromLookup(body []byte, lookup pdfObjectLookup) []pdfEncodedStream {
	pages, rooted := pdfDocumentPages(lookup)
	if !rooted {
		return pdfFallbackDescriptionStreams(body, lookup.objects)
	}
	streams := make([]pdfEncodedStream, 0, 16)
	resources := make([][]byte, 0, len(pages))
	for _, page := range pages {
		if len(streams) >= pdfMaxStreams {
			break
		}
		pageResources := pdfPageResourceDictionary(page.value, lookup)
		resources = append(resources, pageResources)
		references := pdfResolvedPageContentReferences(
			pdfPageContentsReferences(page.value), lookup.byReference,
		)
		for _, reference := range references {
			if len(streams) >= pdfMaxStreams {
				break
			}
			stream, exists := pdfEncodedStreamOf(lookup.value(reference))
			if exists {
				stream.resources = pageResources
				stream.scope = "page " + page.reference
				streams = append(streams, stream)
			}
		}
	}

	return pdfAppendFormStreams(streams, resources, lookup)
}

func pdfPageContentsReferences(dictionary []byte) []string {
	value := pdfDictionaryEntryValue(dictionary, "Contents")
	if reference := pdfLeadingReference(value); reference != "" {
		return []string{reference}
	}

	return pdfArrayReferences(value, pdfMaxStreams)
}

func pdfAppendUniqueReferences(current, additional []string, limit int) []string {
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
		children := pdfAppendObjectReferences(
			nil, pdfArrayReferences(object.value, pdfMaxStreams), queued,
		)
		queue = append(children, queue...)
	}

	return resolved
}

func pdfSelectedObjectStreams(lookup pdfObjectLookup, references []string) []pdfEncodedStream {
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
