package searchlocal

import "github.com/D4rk4/yago/yagonode/internal/searchindex"

func strictCandidateWindowFilled(
	set searchindex.SearchResultSet,
	limit int,
	withFacets bool,
) bool {
	return !withFacets && limit > 0 && len(set.Results) >= limit &&
		set.Total > limit
}
