package documentsearch

// Deliberate divergence from YaCy: only its hardcoded common-word blocklist is
// applied, not the larger stopword and badword sets.
var unhelpfulTopicWords = map[string]struct{}{
	"http": {}, "html": {}, "php": {}, "ftp": {}, "www": {}, "com": {},
	"org": {}, "net": {}, "gov": {}, "edu": {}, "index": {}, "home": {},
	"page": {}, "for": {}, "usage": {}, "the": {}, "and": {}, "zum": {},
	"der": {}, "die": {}, "das": {}, "und": {}, "zur": {}, "bzw": {},
	"mit": {}, "blog": {}, "wiki": {}, "aus": {}, "bei": {}, "off": {},
}

func isUnhelpfulTopicWord(word string) bool {
	_, found := unhelpfulTopicWords[word]

	return found
}
