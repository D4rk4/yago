package formatparse

import (
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func dispatchRegisteredFamily(
	rawURL string,
	contentType string,
	body []byte,
	toggles yagocrawlcontract.FormatToggles,
) (pageparse.ParsedPage, bool, bool) {
	extension := urlExtension(rawURL)
	mime := mimeType(contentType)
	registered := families()
	for _, entry := range registered {
		if entry.extensions[extension] {
			if entry.name == "text" && sniffedPlainTextExtensions[extension] &&
				!bodyAllowsTextFallback(body) {
				continue
			}
			page, parsed := entry.dispatch(rawURL, contentType, body, toggles)

			return page, parsed, true
		}
	}
	for _, entry := range registered {
		if entry.mimes[mime] {
			if entry.name == "text" && strings.HasPrefix(mime, "text/") &&
				!bodyAllowsTextFallback(body) {
				continue
			}
			page, parsed := entry.dispatch(rawURL, contentType, body, toggles)

			return page, parsed, true
		}
	}

	return pageparse.ParsedPage{}, false, false
}
