package adminui

import (
	"net/url"
	"strings"
)

const (
	adminSearchContentDomainParameter = "contentdom"
	adminSearchLanguageParameter      = "language"
	adminSearchSiteHostParameter      = "sitehost"
)

type SearchFilters struct {
	ContentDomain string
	Language      string
	SiteHost      string
}

type searchContentDomainOption struct {
	Value string
	Label string
}

var searchContentDomainOptions = []searchContentDomainOption{
	{Value: "text", Label: "Text"},
	{Value: "all", Label: "All content"},
	{Value: "image", Label: "Images"},
	{Value: "audio", Label: "Audio"},
	{Value: "video", Label: "Video"},
	{Value: "app", Label: "Applications"},
}

func searchFiltersFromValues(values url.Values) SearchFilters {
	return SearchFilters{
		ContentDomain: strings.TrimSpace(values.Get(adminSearchContentDomainParameter)),
		Language:      strings.TrimSpace(values.Get(adminSearchLanguageParameter)),
		SiteHost:      strings.TrimSpace(values.Get(adminSearchSiteHostParameter)),
	}
}

func (filters SearchFilters) addToValues(values url.Values) {
	if filters.ContentDomain != "" {
		values.Set(adminSearchContentDomainParameter, filters.ContentDomain)
	}
	if filters.Language != "" {
		values.Set(adminSearchLanguageParameter, filters.Language)
	}
	if filters.SiteHost != "" {
		values.Set(adminSearchSiteHostParameter, filters.SiteHost)
	}
}
