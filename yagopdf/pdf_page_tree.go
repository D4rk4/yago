package yagopdf

import "bytes"

type pdfPageTreeNode struct {
	reference string
	depth     int
}

func pdfDocumentPages(lookup pdfObjectLookup) ([]pdfIndirectObject, bool) {
	root, _ := pdfTrailerEntry(lookup.trailers, "Root")
	catalog := lookup.value(pdfLeadingReference(root))
	pagesReference := pdfLeadingReference(lookup.entry(catalog, "Pages"))
	if !lookup.hasName(catalog, "Type", "Catalog") ||
		!lookup.hasName(lookup.value(pagesReference), "Type", "Pages") {
		return pdfScannedPages(lookup)
	}
	pages := make([]pdfIndirectObject, 0, 16)
	stack := []pdfPageTreeNode{{reference: pagesReference}}
	visited := make(map[string]struct{}, 16)
	for len(stack) > 0 && len(visited) < pdfMaxIndirectObjects {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, exists := visited[node.reference]; exists || node.depth >= pdfMaxStructureDepth {
			continue
		}
		visited[node.reference] = struct{}{}
		object := lookup.byReference[node.reference]
		if lookup.hasName(object.value, "Type", "Page") {
			pages = append(pages, object)

			continue
		}
		if !lookup.hasName(object.value, "Type", "Pages") {
			continue
		}
		kids := lookup.entry(object.value, "Kids")
		if reference := pdfLeadingReference(kids); reference != "" {
			kids = lookup.value(reference)
		}
		references := pdfArrayReferences(kids, pdfMaxIndirectObjects)
		for index := len(references) - 1; index >= 0 && len(stack) < pdfMaxIndirectObjects; index-- {
			stack = append(
				stack,
				pdfPageTreeNode{reference: references[index], depth: node.depth + 1},
			)
		}
	}

	return pages, true
}

func pdfScannedPages(lookup pdfObjectLookup) ([]pdfIndirectObject, bool) {
	pages := make([]pdfIndirectObject, 0, 16)
	for _, object := range lookup.objects {
		if lookup.hasName(object.value, "Type", "Page") {
			pages = append(pages, object)
		}
	}

	return pages, len(pages) > 0
}

func pdfArrayReferences(value []byte, limit int) []string {
	value = bytes.TrimSpace(value)
	if len(value) < 2 || value[0] != '[' {
		return nil
	}
	end := pdfCompositeEnd(value, 0)
	if value[end-1] != ']' {
		return nil
	}
	references := make([]string, 0, min(limit, 16))
	for at := 1; at < end-1 && len(references) < limit; {
		at = pdfSkipSpace(value, at)
		if reference, next := pdfReferenceAt(value, at); next > at {
			references = append(references, reference)
			at = next
		} else {
			at = pdfValueEnd(value, at)
		}
	}

	return references
}
