package formatparse

import "bytes"

func pdfPageResourceDictionary(dictionary []byte, lookup pdfObjectLookup) []byte {
	visited := make(map[string]struct{}, 8)
	for {
		if resources := pdfDictionaryForEntry(dictionary, "Resources", lookup); resources != nil {
			return resources
		}
		parent := pdfReferenceForEntry(dictionary, "Parent")
		if parent == "" {
			return nil
		}
		if _, exists := visited[parent]; exists {
			return nil
		}
		visited[parent] = struct{}{}
		dictionary = lookup.value(parent)
		if dictionary == nil {
			return nil
		}
	}
}

func pdfReachableFormReferences(
	resourceDictionaries [][]byte,
	lookup pdfObjectLookup,
) []string {
	queued := make(map[string]struct{}, pdfMaxIndirectObjects)
	queue := make([]string, 0, 16)
	for _, resources := range resourceDictionaries {
		queue = pdfAppendObjectReferences(queue, pdfXObjectReferences(resources, lookup), queued)
	}
	forms := make([]string, 0, len(queue))
	for len(queue) > 0 {
		reference := queue[0]
		queue = queue[1:]
		stream, exists := pdfEncodedStreamOf(lookup.value(reference))
		if !exists || !pdfFormSubtypePattern.Match(stream.dictionary) {
			continue
		}
		forms = append(forms, reference)
		resources := pdfDictionaryForEntry(stream.dictionary, "Resources", lookup)
		if resources != nil {
			queue = pdfAppendObjectReferences(
				queue,
				pdfXObjectReferences(resources, lookup),
				queued,
			)
		}
	}

	return forms
}

func pdfAppendObjectReferences(
	current []string,
	additional []string,
	seen map[string]struct{},
) []string {
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

func pdfXObjectReferences(resources []byte, lookup pdfObjectLookup) []string {
	xObjects := pdfDictionaryForEntry(resources, "XObject", lookup)
	if xObjects == nil {
		return nil
	}

	return pdfReferences(xObjects)
}

func pdfDictionaryForEntry(
	dictionary []byte,
	name string,
	lookup pdfObjectLookup,
) []byte {
	value := pdfDictionaryEntryValue(dictionary, name)
	if value == nil {
		return nil
	}
	if direct := pdfDirectDictionary(value); direct != nil {
		return direct
	}
	reference := pdfLeadingReference(value)
	if reference == "" {
		return nil
	}

	return pdfDirectDictionary(lookup.value(reference))
}

func pdfReferenceForEntry(dictionary []byte, name string) string {
	return pdfLeadingReference(pdfDictionaryEntryValue(dictionary, name))
}

func pdfLeadingReference(value []byte) string {
	value = bytes.TrimLeft(value, "\t \r\n")
	match := pdfLeadingReferencePattern.FindSubmatch(value)
	if match == nil {
		return ""
	}

	return string(match[1]) + " " + string(match[2])
}

func pdfDirectDictionary(value []byte) []byte {
	value = bytes.TrimLeft(value, "\t \r\n")
	length := pdfDictionaryLength(value)
	if length == 0 {
		return nil
	}

	return value[:length]
}

func pdfDictionaryEntryValue(dictionary []byte, name string) []byte {
	depth := 0
	for at := 0; at < len(dictionary); {
		switch dictionary[at] {
		case '%':
			at = pdfLineEnd(dictionary, at+1)
		case '(':
			_, consumed := pdfRawStringLiteralWithin(dictionary[at:], 0)
			at += consumed + 1
		case '<':
			if at+1 < len(dictionary) && dictionary[at+1] == '<' {
				depth++
				at += 2

				continue
			}
			at = pdfHexEnd(dictionary, at+1)
		case '>':
			if at+1 < len(dictionary) && dictionary[at+1] == '>' {
				depth--
				at += 2
				if depth == 0 {
					return nil
				}

				continue
			}
			at++
		case '/':
			token, consumed := pdfName(dictionary[at:])
			at += consumed + 1
			if depth == 1 && token == name {
				return bytes.TrimLeft(dictionary[at:], "\t \r\n")
			}
		default:
			at++
		}
	}

	return nil
}

func pdfDictionaryLength(dictionary []byte) int {
	if len(dictionary) < 4 || dictionary[0] != '<' || dictionary[1] != '<' {
		return 0
	}
	depth := 0
	for at := 0; at < len(dictionary); {
		switch dictionary[at] {
		case '%':
			at = pdfLineEnd(dictionary, at+1)
		case '(':
			_, consumed := pdfRawStringLiteralWithin(dictionary[at:], 0)
			at += consumed + 1
		case '<':
			if at+1 < len(dictionary) && dictionary[at+1] == '<' {
				depth++
				at += 2

				continue
			}
			at = pdfHexEnd(dictionary, at+1)
		case '>':
			if at+1 < len(dictionary) && dictionary[at+1] == '>' {
				depth--
				at += 2
				if depth == 0 {
					return at
				}

				continue
			}
			at++
		default:
			at++
		}
	}

	return 0
}

func pdfLineEnd(value []byte, at int) int {
	for at < len(value) && value[at] != '\r' && value[at] != '\n' {
		at++
	}

	return at
}

func pdfHexEnd(value []byte, at int) int {
	end := bytes.IndexByte(value[at:], '>')
	if end < 0 {
		return len(value)
	}

	return at + end + 1
}
