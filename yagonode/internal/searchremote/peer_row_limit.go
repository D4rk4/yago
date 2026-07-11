package searchremote

import "github.com/D4rk4/yago/yagomodel"

func cappedPeerRows(
	rows []yagomodel.URIMetadataRow,
	limit int,
) []yagomodel.URIMetadataRow {
	if limit > 0 && len(rows) > limit {
		return rows[:limit]
	}

	return rows
}
