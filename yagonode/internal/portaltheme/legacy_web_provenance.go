package portaltheme

import "strings"

const (
	legacyWebFooterFragment  = `<span class="prov prov-ddgs">[ddgs]</span> an external provider`
	currentWebFooterFragment = `<span class="prov prov-web">web</span> an external provider`
	legacyWebCountFragment   = `{{results.webCount}} from DDGS.`
	currentWebCountFragment  = `{{results.webCount}} from the web.`
	legacyWebStyleFragment   = `.prov-ddgs { border-color: #ffd6e8; background: #fff0f7; color: #740937; }`
	currentWebStyleFragment  = `.prov-web { border-color: #ffd6e8; background: #fff0f7; color: #740937; }`
)

func repairLegacyPortalDocument(page, body string) string {
	if page != SharedStyles {
		body = repairLegacyVisualScript(body)
		body = repairLegacyOpenSearchDiscovery(body)
		body = repairLegacyResultTotal(body)
		body = repairLegacySearchTruth(body)
		body = strings.ReplaceAll(body, legacyWebFooterFragment, currentWebFooterFragment)
		body = strings.ReplaceAll(body, legacyWebCountFragment, currentWebCountFragment)

		return body
	}

	return strings.ReplaceAll(body, legacyWebStyleFragment, currentWebStyleFragment)
}
