package adminui

import (
	"net/url"
	"strconv"
)

const activityEntriesPerPage = 20

type ActivityPagination struct {
	Entries []ActivityEntry
	Total   int
	Page    int
	Pages   int
	Start   int
	End     int
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
}

func buildActivityPagination(entries []ActivityEntry, rawPage string) ActivityPagination {
	total := len(entries)
	pages := (total + activityEntriesPerPage - 1) / activityEntriesPerPage
	if pages < 1 {
		pages = 1
	}
	page := requestedActivityPage(rawPage)
	if page > pages {
		page = pages
	}
	start := (page - 1) * activityEntriesPerPage
	end := start + activityEntriesPerPage
	if end > total {
		end = total
	}

	visibleEntries := make([]ActivityEntry, end-start)
	copy(visibleEntries, entries[start:end])
	pagination := ActivityPagination{
		Entries: visibleEntries,
		Total:   total,
		Page:    page,
		Pages:   pages,
		HasPrev: page > 1,
		HasNext: page < pages,
	}
	if total > 0 {
		pagination.Start = start + 1
		pagination.End = end
	}
	if pagination.HasPrev {
		pagination.PrevURL = activityPageURL(page - 1)
	}
	if pagination.HasNext {
		pagination.NextURL = activityPageURL(page + 1)
	}

	return pagination
}

func requestedActivityPage(rawPage string) int {
	page, err := strconv.Atoi(rawPage)
	if err != nil || page < 1 {
		return 1
	}

	return page
}

func activityPageURL(page int) string {
	values := url.Values{}
	if page > 1 {
		values.Set("apage", strconv.Itoa(page))
	}
	target := activityPath
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}

	return target + "#recent-searches"
}
