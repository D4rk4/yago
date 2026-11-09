package yagopdf

import "strconv"

type pdfFormScope struct {
	reference string
	resources []byte
}

type pdfFormIdentity struct {
	reference string
	resources pdfDictionaryIdentity
}

func pdfAppendFormStreams(
	streams []pdfEncodedStream,
	resources [][]byte,
	lookup pdfObjectLookup,
) []pdfEncodedStream {
	queue := make([]pdfFormScope, 0, 16)
	queued := make(map[pdfFormIdentity]struct{}, 16)
	visited := make(map[pdfFormIdentity]struct{}, 16)
	resourceScopes := make(map[pdfDictionaryIdentity]struct{}, len(resources))
	for _, dictionary := range resources {
		identity := pdfDictionaryIdentityOf(dictionary)
		if _, exists := resourceScopes[identity]; exists {
			continue
		}
		resourceScopes[identity] = struct{}{}
		queue = pdfQueueForms(queue, dictionary, lookup, queued)
	}
	for len(queue) > 0 && len(streams) < pdfMaxStreams {
		form := queue[0]
		queue = queue[1:]
		stream, exists := pdfEncodedStreamOf(lookup.value(form.reference))
		if !exists || !lookup.hasName(stream.dictionary, "Subtype", "Form") {
			continue
		}
		stream.resources = pdfDictionaryForEntry(stream.dictionary, "Resources", lookup)
		if stream.resources == nil {
			stream.resources = form.resources
		}
		identity := pdfFormIdentity{
			reference: form.reference, resources: pdfDictionaryIdentityOf(stream.resources),
		}
		if _, exists := visited[identity]; exists {
			continue
		}
		visited[identity] = struct{}{}
		stream.scope = "form " + form.reference + " " + strconv.Itoa(len(visited))
		streams = append(streams, stream)
		queue = pdfQueueForms(queue, stream.resources, lookup, queued)
	}

	return streams
}

func pdfQueueForms(
	queue []pdfFormScope,
	resources []byte,
	lookup pdfObjectLookup,
	queued map[pdfFormIdentity]struct{},
) []pdfFormScope {
	for _, reference := range pdfXObjectReferences(resources, lookup) {
		if len(queued) >= pdfMaxIndirectObjects {
			break
		}
		identity := pdfFormIdentity{
			reference: reference,
			resources: pdfDictionaryIdentityOf(resources),
		}
		if _, exists := queued[identity]; exists {
			continue
		}
		queued[identity] = struct{}{}
		queue = append(queue, pdfFormScope{reference: reference, resources: resources})
	}

	return queue
}
