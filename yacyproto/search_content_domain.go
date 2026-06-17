package yacyproto

import "fmt"

type SearchContentDomain string

const (
	ContentDomainAll   SearchContentDomain = "all"
	ContentDomainText  SearchContentDomain = "text"
	ContentDomainImage SearchContentDomain = "image"
	ContentDomainAudio SearchContentDomain = "audio"
	ContentDomainVideo SearchContentDomain = "video"
	ContentDomainApp   SearchContentDomain = "app"
	ContentDomainCtrl  SearchContentDomain = "ctrl"
)

func parseSearchContentDomain(raw string) (SearchContentDomain, error) {
	if raw == "" {
		return "", nil
	}

	domain := SearchContentDomain(raw)
	switch domain {
	case ContentDomainAll,
		ContentDomainText,
		ContentDomainImage,
		ContentDomainAudio,
		ContentDomainVideo,
		ContentDomainApp,
		ContentDomainCtrl:
		return domain, nil
	default:
		return "", fmt.Errorf("%w: search request %s=%q", ErrBadField, FieldContentDom, raw)
	}
}
