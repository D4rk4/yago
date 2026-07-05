package searchcore

import "sort"

// OrderByDateWhenRequested re-orders results newest-first when the query
// carried the /date modifier (YaCy's date sort). Dates are YaCy's yyyyMMdd
// wire strings, so lexicographic comparison orders them chronologically;
// undated results keep their relevance order after every dated one. The
// re-order happens before any offset window is cut, so paging stays stable.
func OrderByDateWhenRequested(results []Result, req Request) {
	if !req.SortByDate {
		return
	}
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i].Date, results[j].Date
		if b == "" {
			return a != ""
		}
		if a == "" {
			return false
		}

		return a > b
	})
}
