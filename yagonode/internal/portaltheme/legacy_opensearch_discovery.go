package portaltheme

import "strings"

const (
	legacyOpenSearchAdvertisement  = `<link rel="search" type="application/opensearchdescription+xml" title="{{brand}} search" href="/opensearch.xml">`
	currentOpenSearchAdvertisement = `<link rel="search" type="application/opensearchdescription+xml" title="{{openSearchTitle}}" href="/opensearch.xml">`
)

func repairLegacyOpenSearchDiscovery(body string) string {
	return strings.ReplaceAll(
		body,
		legacyOpenSearchAdvertisement,
		currentOpenSearchAdvertisement,
	)
}
