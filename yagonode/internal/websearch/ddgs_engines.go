package websearch

import (
	"net/url"
	"strings"
)

const (
	engineMojeek  = "mojeek"
	engineBing    = "bing"
	engineDDGHTML = "ddg-html"
	engineDDGLite = "ddg-lite"
	engineBrave   = "brave"

	backendAuto       = "auto"
	backendMojeek     = "mojeek"
	backendBing       = "bing"
	backendDuckDuckGo = "duckduckgo"
	backendDDG        = "ddg"
	backendBrave      = "brave"
)

type engine struct {
	name     string
	endpoint string
	queryKey string
	parse    func([]byte) ([]Result, error)
	safe     func(mode string) url.Values
}

func allEngines() map[string]engine {
	return map[string]engine{
		engineMojeek: {
			name:     engineMojeek,
			endpoint: "https://www.mojeek.com/search",
			queryKey: "q",
			parse:    parseListResults,
			safe:     mojeekSafeParams,
		},
		engineBing: {
			name:     engineBing,
			endpoint: "https://www.bing.com/search",
			queryKey: "q",
			parse:    parseListResults,
			safe:     noSafeParams,
		},
		engineDDGHTML: {
			name:     engineDDGHTML,
			endpoint: "https://html.duckduckgo.com/html/",
			queryKey: "q",
			parse:    parseDuckDuckGoResults,
			safe:     duckSafeParams,
		},
		engineDDGLite: {
			name:     engineDDGLite,
			endpoint: "https://lite.duckduckgo.com/lite/",
			queryKey: "q",
			parse:    parseDuckDuckGoLiteResults,
			safe:     duckSafeParams,
		},
		engineBrave: {
			name:     engineBrave,
			endpoint: "https://search.brave.com/search",
			queryKey: "q",
			parse:    parseBraveResults,
			safe:     braveSafeParams,
		},
	}
}

func backendsFor(name string) []engine {
	engines := allEngines()
	switch strings.ToLower(strings.TrimSpace(name)) {
	case backendMojeek:
		return []engine{engines[engineMojeek]}
	case backendBing:
		return []engine{engines[engineBing]}
	case backendDuckDuckGo, backendDDG:
		return []engine{engines[engineDDGHTML], engines[engineDDGLite]}
	case backendBrave:
		return []engine{engines[engineBrave]}
	default:
		return []engine{
			engines[engineDDGHTML],
			engines[engineDDGLite],
			engines[engineBrave],
			engines[engineMojeek],
			engines[engineBing],
		}
	}
}

func (e engine) params(query, safeSearch string) url.Values {
	values := url.Values{}
	if e.safe != nil {
		values = e.safe(safeSearch)
	}
	values.Set(e.queryKey, query)

	return values
}

func mojeekSafeParams(mode string) url.Values {
	values := url.Values{}
	if strings.EqualFold(strings.TrimSpace(mode), "off") {
		values.Set("safe", "0")
	} else {
		values.Set("safe", "1")
	}

	return values
}

func duckSafeParams(mode string) url.Values {
	values := url.Values{}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "strict":
		values.Set("kp", "1")
	case "off":
		values.Set("kp", "-1")
	}

	return values
}

func noSafeParams(string) url.Values { return url.Values{} }

func braveSafeParams(mode string) url.Values {
	values := url.Values{}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "strict":
		values.Set("safesearch", "strict")
	case "off":
		values.Set("safesearch", "off")
	}

	return values
}
