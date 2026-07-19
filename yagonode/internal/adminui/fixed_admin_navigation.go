package adminui

import (
	"net/http"
	"net/url"
	"strconv"
)

func adminSearchPageURL(query string, global bool, page int) string {
	return filteredAdminSearchPageURL(query, global, SearchFilters{}, page)
}

func filteredAdminSearchPageURL(
	query string,
	global bool,
	filters SearchFilters,
	page int,
) string {
	scope := "global"
	if !global {
		scope = "local"
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("scope", scope)
	values.Set("p", strconv.Itoa(page))
	filters.addToValues(values)

	return (&url.URL{Path: searchPath, RawQuery: values.Encode()}).String()
}

func redirectFilteredAdminSearchPage(
	w http.ResponseWriter,
	query string,
	global bool,
	filters SearchFilters,
	page int,
) {
	w.Header().Set("Location", filteredAdminSearchPageURL(query, global, filters, page))
	w.WriteHeader(http.StatusSeeOther)
}

func redirectAdminSearchPage(
	w http.ResponseWriter,
	query string,
	global bool,
	page int,
) {
	w.Header().Set("Location", adminSearchPageURL(query, global, page))
	w.WriteHeader(http.StatusSeeOther)
}

func redirectCrawlRunPage(w http.ResponseWriter, page int) {
	w.Header().Set("Location", crawlRunPageURL(page))
	w.WriteHeader(http.StatusSeeOther)
}
