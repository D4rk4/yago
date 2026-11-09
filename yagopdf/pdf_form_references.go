package yagopdf

func pdfPageResourceDictionary(dictionary []byte, lookup pdfObjectLookup) []byte {
	path := make([]pdfDictionaryIdentity, 0, pdfMaxStructureDepth)
	visited := make(map[pdfDictionaryIdentity]struct{}, pdfMaxStructureDepth)
	result := pdfInheritedResources{}
	for len(path) < pdfMaxStructureDepth {
		identity := pdfDictionaryIdentityOf(dictionary)
		if cached, exists := lookup.resources[identity]; exists &&
			(cached.complete || cached.depth >= pdfMaxStructureDepth-len(path)) {
			result = cached

			break
		}
		if _, exists := visited[identity]; exists || len(dictionary) == 0 {
			result.complete = true

			break
		}
		visited[identity] = struct{}{}
		path = append(path, identity)
		if resources := pdfDictionaryForEntry(dictionary, "Resources", lookup); resources != nil {
			result = pdfInheritedResources{resources: resources, depth: -1, complete: true}

			break
		}
		parent := pdfLeadingReference(lookup.entry(dictionary, "Parent"))
		dictionary = lookup.value(parent)
	}
	for index := len(path) - 1; index >= 0; index-- {
		result.depth++
		if lookup.resources != nil {
			lookup.resources[path[index]] = result
		}
	}
	if result.depth >= pdfMaxStructureDepth {
		return nil
	}

	return result.resources
}

func pdfXObjectReferences(resources []byte, lookup pdfObjectLookup) []string {
	identity := pdfDictionaryIdentityOf(resources)
	if references, exists := lookup.xObjects[identity]; exists {
		return references
	}
	xObjects := pdfDictionaryForEntry(resources, "XObject", lookup)
	cursor := newPDFDictionaryCursor(xObjects)
	references := make([]string, 0, 8)
	for len(references) < pdfMaxIndirectObjects {
		_, value, exists := cursor.next()
		if !exists {
			break
		}
		if reference := pdfLeadingReference(value); reference != "" {
			references = append(references, reference)
		}
	}
	if lookup.xObjects != nil {
		lookup.xObjects[identity] = references
	}

	return references
}

func pdfAppendObjectReferences(current, additional []string, seen map[string]struct{}) []string {
	for _, reference := range additional {
		if len(seen) >= pdfMaxIndirectObjects {
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
