package documentsearch

import (
	"maps"

	"github.com/D4rk4/yago/yagomodel"
)

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
		properties[yagomodel.URLMetaWordReference] = yagomodel.Encode(
			[]byte(yagomodel.WordReferencePropertyForm(document.posting)),
		)
		enriched[position] = yagomodel.URIMetadataRow{Properties: properties}
	}

	return enriched
}

func resourcesWithDefaultWordReferences(
	resources []yagomodel.URIMetadataRow,
) []yagomodel.URIMetadataRow {
	enriched := make([]yagomodel.URIMetadataRow, len(resources))
	for position, resource := range resources {
		if resource.Properties[yagomodel.URLMetaWordReference] != "" {
			enriched[position] = resource
			continue
		}
		identifier, err := resource.URLHash()
		if err != nil {
			enriched[position] = resource
			continue
		}
		properties := make(map[string]string, len(resource.Properties)+1)
		maps.Copy(properties, resource.Properties)
		properties[yagomodel.URLMetaWordReference] = yagomodel.Encode([]byte(
			yagomodel.WordReferencePropertyForm(yagomodel.RWIPosting{Properties: map[string]string{
				yagomodel.ColURLHash: identifier.String(),
			}}),
		))
		enriched[position] = yagomodel.URIMetadataRow{Properties: properties}
	}

	return enriched
}
