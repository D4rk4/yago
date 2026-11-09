package yagopdf

type pdfDictionaryIdentity struct {
	start  *byte
	length int
}

type pdfDictionaryEntry struct {
	dictionary pdfDictionaryIdentity
	name       string
}

type pdfInheritedResources struct {
	resources []byte
	depth     int
	complete  bool
}

type pdfObjectLookup struct {
	objects      []pdfIndirectObject
	trailers     [][]byte
	byReference  map[string]pdfIndirectObject
	dictionaries map[pdfDictionaryIdentity][]byte
	entries      map[pdfDictionaryEntry][]byte
	resources    map[pdfDictionaryIdentity]pdfInheritedResources
	xObjects     map[pdfDictionaryIdentity][]string
}

func newPDFObjectLookup(body []byte) pdfObjectLookup {
	structure := pdfScanFileStructure(body)
	objects := structure.objects
	byReference := make(map[string]pdfIndirectObject, len(objects))
	for _, object := range objects {
		byReference[object.reference] = object
	}

	return pdfObjectLookup{
		objects: objects, trailers: structure.trailers, byReference: byReference,
		dictionaries: make(map[pdfDictionaryIdentity][]byte),
		entries:      make(map[pdfDictionaryEntry][]byte),
		resources:    make(map[pdfDictionaryIdentity]pdfInheritedResources),
		xObjects:     make(map[pdfDictionaryIdentity][]string),
	}
}

func pdfDictionaryIdentityOf(value []byte) pdfDictionaryIdentity {
	if len(value) == 0 {
		return pdfDictionaryIdentity{}
	}

	return pdfDictionaryIdentity{start: &value[0], length: len(value)}
}

func (l pdfObjectLookup) value(reference string) []byte {
	return l.byReference[reference].value
}

func (l pdfObjectLookup) dictionary(value []byte) []byte {
	identity := pdfDictionaryIdentityOf(value)
	if dictionary, exists := l.dictionaries[identity]; exists {
		return dictionary
	}
	dictionary := pdfDirectDictionary(value)
	if l.dictionaries != nil {
		l.dictionaries[identity] = dictionary
	}

	return dictionary
}

func (l pdfObjectLookup) entry(dictionary []byte, name string) []byte {
	key := pdfDictionaryEntry{dictionary: pdfDictionaryIdentityOf(dictionary), name: name}
	if value, exists := l.entries[key]; exists {
		return value
	}
	value := pdfDictionaryEntryValue(dictionary, name)
	if l.entries != nil {
		l.entries[key] = value
	}

	return value
}

func pdfDictionaryForEntry(dictionary []byte, name string, lookup pdfObjectLookup) []byte {
	value := lookup.entry(dictionary, name)
	if reference := pdfLeadingReference(value); reference != "" {
		value = lookup.value(reference)
	}

	return lookup.dictionary(value)
}

func (l pdfObjectLookup) hasName(dictionary []byte, entry, name string) bool {
	actual, _, valid := pdfDecodedNameToken(l.entry(dictionary, entry), pdfMaxPDFNameBytes)

	return valid && actual == name
}
