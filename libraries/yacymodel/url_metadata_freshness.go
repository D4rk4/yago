package yacymodel

const (
	ColLoadDate  = "load"
	ColModDate   = "mod"
	ColFreshDate = "fresh"
)

var freshnessPrecedence = []string{ColLoadDate, ColModDate, ColFreshDate}

func (r URIMetadataRow) Freshness() string {
	for _, key := range freshnessPrecedence {
		if value := r.Properties[key]; value != "" {
			return value
		}
	}

	return ""
}
