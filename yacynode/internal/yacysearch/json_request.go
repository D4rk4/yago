package yacysearch

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/D4rk4/yago/yacynode/internal/searchcore"
	"github.com/D4rk4/yago/yacyproto"
)

const publicSearchLimitCap = 10

func searchRequestFromValues(values url.Values) (searchcore.Request, error) {
	parsed := searchcore.ParseTextQuery(values.Get(yacyproto.FieldQuery))
	limit, err := optionalRequestInt(values, yacyproto.FieldMaximumRecords)
	if err != nil {
		return searchcore.Request{}, err
	}
	offset, err := optionalRequestInt(values, yacyproto.FieldStartRecord)
	if err != nil {
		return searchcore.Request{}, err
	}

	req, err := searchcore.NormalizePublicRequest(searchcore.Request{
		Query:            values.Get(yacyproto.FieldQuery),
		Terms:            parsed.Terms,
		ExcludedTerms:    parsed.ExcludedTerms,
		Source:           searchcore.Source(values.Get(yacyproto.FieldResource)),
		Limit:            limit,
		Offset:           offset,
		ContentDomain:    searchcore.ContentDomain(values.Get(yacyproto.FieldContentDom)),
		Language:         firstNonEmpty(values.Get(yacyproto.FieldLanguage), parsed.Language),
		SiteHost:         firstNonEmpty(values.Get(yacyproto.FieldSiteHost), parsed.SiteHost),
		InURL:            parsed.InURL,
		TLD:              parsed.TLD,
		FileType:         firstNonEmpty(values.Get(yacyproto.FieldFileType), parsed.FileType),
		URLMaskFilter:    values.Get(yacyproto.FieldURLMaskFilter),
		PreferMaskFilter: values.Get(yacyproto.FieldPreferMaskFilter),
		Verify:           searchcore.VerifyMode(values.Get(yacyproto.FieldVerify)),
		Navigation:       values.Get(yacyproto.FieldNavigation),
		SortByDate:       parsed.SortByDate,
		Near:             parsed.Near,
	}, publicSearchLimitCap)
	if err != nil {
		return searchcore.Request{}, fmt.Errorf("public search request: %w", err)
	}

	return req, nil
}

func optionalRequestInt(values url.Values, key string) (int, error) {
	raw := values.Get(key)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}

	return value, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
