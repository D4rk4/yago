package snippetfetch

import (
	"strings"
	"unicode/utf8"
)

const excerptRuneCap = 320

func evidenceExcerpt(text string, evidence TextEvidence) (string, bool) {
	if evidence.Start < 0 || evidence.End <= evidence.Start || evidence.End > len(text) ||
		!utf8.ValidString(text) || !utf8.ValidString(text[evidence.Start:evidence.End]) {
		return "", false
	}
	prefix := []rune(text[:evidence.Start])
	witness := []rune(text[evidence.Start:evidence.End])
	suffix := []rune(text[evidence.End:])
	if len(witness) > excerptRuneCap {
		return "", false
	}
	remaining := excerptRuneCap - len(witness)
	lead := min(len(prefix), remaining/4)
	trail := min(len(suffix), remaining-lead)
	lead = min(len(prefix), lead+remaining-lead-trail)
	window := make([]rune, 0, lead+len(witness)+trail)
	window = append(window, prefix[len(prefix)-lead:]...)
	window = append(window, witness...)
	window = append(window, suffix[:trail]...)
	excerpt := strings.Join(strings.Fields(string(window)), " ")
	if lead < len(prefix) && utf8.RuneCountInString(excerpt) <= excerptRuneCap-2 {
		excerpt = "… " + excerpt
	}

	return excerpt, excerpt != ""
}
