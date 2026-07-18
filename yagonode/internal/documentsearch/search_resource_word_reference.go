package documentsearch

import (
	"maps"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
)

const searchResourceWordReference = "wi"

var wordReferencePropertyColumns = []struct {
	name         string
	defaultValue string
}{
	{yagomodel.ColURLHash, ""},
	{yagomodel.ColLastModified, "0"},
	{yagomodel.ColFreshUntil, "0"},
	{yagomodel.ColTitleWordCount, "0"},
	{yagomodel.ColTextWordCount, "0"},
	{yagomodel.ColPhraseCount, "0"},
	{yagomodel.ColDocType, "0"},
	{yagomodel.ColLanguage, "en"},
	{yagomodel.ColLocalLinkCount, "0"},
	{yagomodel.ColExternalLinkCount, "0"},
	{yagomodel.ColURLLength, "0"},
	{yagomodel.ColURLComponentCount, "0"},
	{yagomodel.ColWordType, "0"},
	{yagomodel.ColFlags, "AAAAAA"},
	{yagomodel.ColHitCount, "0"},
	{yagomodel.ColTextPosition, "0"},
	{yagomodel.ColPhraseRelativePos, "0"},
	{yagomodel.ColPhrasePosition, "0"},
	{yagomodel.ColWordDistance, "0"},
	{yagomodel.ColReserve, "0"},
}

func resourcesWithWordReferences(
	resources []yagomodel.URIMetadataRow,
	documents map[yagomodel.Hash]matchedDocument,
) []yagomodel.URIMetadataRow {
	enriched := make([]yagomodel.URIMetadataRow, len(resources))
	for position, resource := range resources {
		identifier, err := resource.URLHash()
		document, found := documents[identifier.Hash()]
		if err != nil || !found {
			enriched[position] = resource
			continue
		}
		properties := make(map[string]string, len(resource.Properties)+1)
		maps.Copy(properties, resource.Properties)
		properties[searchResourceWordReference] = yagomodel.Encode(
			[]byte(postingPropertyForm(document.posting)),
		)
		enriched[position] = yagomodel.URIMetadataRow{Properties: properties}
	}

	return enriched
}

func postingPropertyForm(posting yagomodel.RWIPosting) string {
	var propertyForm strings.Builder
	propertyForm.WriteByte('{')
	for position, column := range wordReferencePropertyColumns {
		if position > 0 {
			propertyForm.WriteByte(',')
		}
		propertyForm.WriteString(column.name)
		propertyForm.WriteByte('=')
		value := posting.Properties[column.name]
		if value == "" {
			value = column.defaultValue
		}
		propertyForm.WriteString(value)
	}
	propertyForm.WriteByte('}')

	return propertyForm.String()
}
