// Package stopwords carries a compact multilingual function-word list for
// query-time decisions: which query words may be required of documents,
// verified against peer results, anchor snippets, or drive highlighting. The
// entries derive from the Snowball per-language stopword lists (the same
// families the index analyzers use), trimmed to the high-frequency words that
// dominate natural-language queries. Index-time analysis keeps its own
// per-language stop filters; this list only guards ranking-adjacent surfaces,
// so an over-broad entry costs a little emphasis, never recall.
package stopwords

import "strings"

// IsStopword reports whether the folded word is a function word in one of the
// bundled languages.
func IsStopword(word string) bool {
	return stopwordSet[strings.ToLower(strings.TrimSpace(word))]
}

// ContentTerms returns the terms that are not stopwords, preserving order. A
// query of only stopwords yields an empty slice — callers fall back to the
// full term list, mirroring YaCy's all-stopword handling.
func ContentTerms(terms []string) []string {
	content := make([]string, 0, len(terms))
	for _, term := range terms {
		if term == "" || IsStopword(term) {
			continue
		}
		content = append(content, term)
	}

	return content
}

func stopwordList() []string {
	return []string{
		// Russian (Snowball stopwords_ru + the pronoun forms Snowball lists only
		// in the masculine: такое/такая/такие beside такой, это beside этот).
		"и", "в", "во", "не", "что", "он", "на", "я", "с", "со", "как", "а",
		"то", "все", "она", "так", "его", "но", "да", "ты", "к", "у", "же",
		"вы", "за", "бы", "по", "только", "ее", "её", "мне", "было", "вот",
		"от", "меня", "еще", "ещё", "нет", "о", "из", "ему", "теперь",
		"когда", "даже", "ну", "вдруг", "ли", "если", "уже", "или", "ни",
		"быть", "был", "него", "до", "вас", "нибудь", "опять", "уж", "вам",
		"ведь", "там", "потом", "себя", "ничего", "ей", "может", "они",
		"тут", "где", "есть", "надо", "ней", "для", "мы", "тебя", "их",
		"чем", "была", "сам", "чтоб", "без", "будто", "чего", "раз", "тоже",
		"себе", "под", "будет", "ж", "тогда", "кто", "этот", "того",
		"потому", "этого", "какой", "какое", "какая", "какие", "совсем",
		"ним", "здесь", "этом", "один", "почти", "мой", "тем", "чтобы",
		"нее", "неё", "сейчас", "были", "куда", "зачем", "всех", "никогда",
		"можно", "при", "наконец", "два", "об", "другой", "хоть", "после",
		"над", "больше", "тот", "через", "эти", "нас", "про", "всего",
		"них", "много", "разве", "три", "эту", "моя", "впрочем", "хорошо",
		"свою", "этой", "перед", "иногда", "лучше", "чуть", "том", "нельзя",
		"такой", "такое", "такая", "такие", "им", "более", "всегда",
		"конечно", "всю", "между", "это",
		// English (Lucene/Snowball core).
		"a", "an", "and", "are", "as", "at", "be", "but", "by", "for", "if",
		"in", "into", "is", "it", "no", "not", "of", "on", "or", "such",
		"that", "the", "their", "then", "there", "these", "they", "this",
		"to", "was", "will", "with", "what", "which", "who", "how", "when",
		"where", "why", "from", "has", "have", "had", "were", "been", "its",
		"about", "can", "do", "does", "did", "you", "your", "we", "our",
		"i", "me", "my", "he", "she", "his", "her", "them", "so", "than",
		// German (Snowball core).
		"der", "die", "das", "den", "dem", "des", "und", "oder", "aber",
		"ein", "eine", "einer", "eines", "einem", "einen", "mit", "von",
		"zu", "im", "am", "um", "auf", "aus", "bei", "nach", "über", "für",
		"ist", "sind", "war", "waren", "wird", "werden", "nicht", "auch",
		"noch", "nur", "wie", "wo", "wer", "wann", "warum", "ich", "du",
		"er", "sie", "es", "wir", "ihr", "sich", "dass", "daß", "als",
		// French (Snowball core).
		"le", "la", "les", "un", "une", "et", "ou", "mais", "dans", "sur",
		"avec", "sans", "pour", "par", "au", "aux", "du", "de", "ce",
		"cette", "ces", "que", "qui", "quoi", "quand", "est", "sont",
		"était", "je", "tu", "il", "elle", "nous", "vous", "ils", "elles",
		"ne", "pas", "plus", "se", "sa", "son", "ses", "leur", "comme",
		// Spanish (Snowball core).
		"el", "los", "las", "uno", "unos", "unas", "y", "o", "pero", "en",
		"con", "sin", "para", "por", "lo", "al", "es", "son", "era", "fue",
		"yo", "tú", "él", "ella", "nosotros", "ellos", "qué", "cómo",
		"cuándo", "dónde", "porque", "muy", "más", "menos", "como", "si",
		"su", "sus", "mi", "mis", "te", "me", "nos", "les", "ya", "también",
	}
}

var stopwordSet = func() map[string]bool {
	set := make(map[string]bool, 512)
	for _, word := range stopwordList() {
		set[word] = true
	}

	return set
}()
