package portaltheme

import "strings"

var legacyVisualScriptReplacements = []struct {
	broken string
	fixed  string
}{
	{"if (!input  !list) return;", "if (!input || !list) return;"},
	{"render((data && data[1])  []);", "render((data && data[1]) || []);"},
}

const legacyAutocompleteScriptStart = `(function () {
  var input = document.getElementById("q");`

const legacyAutocompleteScriptEnd = `
})();`

var defaultAutocompleteScript = func() string {
	start := strings.Index(defaultScripts, legacyAutocompleteScriptStart)
	end := strings.Index(defaultScripts[start:], legacyAutocompleteScriptEnd) +
		start + len(legacyAutocompleteScriptEnd)

	return defaultScripts[start:end]
}()

var legacyBrokenAutocompleteScript = func() string {
	body := defaultAutocompleteScript
	for _, replacement := range legacyVisualScriptReplacements {
		body = strings.ReplaceAll(body, replacement.fixed, replacement.broken)
	}

	return body
}()

func repairLegacyVisualScript(body string) string {
	return strings.ReplaceAll(body, legacyBrokenAutocompleteScript, defaultAutocompleteScript)
}
