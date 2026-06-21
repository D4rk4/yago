package contracts

import (
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func (q SearchQuery) JoinSiteHash() (string, error) {
	if q.Filters.SiteHash != "" {
		return q.Filters.SiteHash, nil
	}
	siteHost := ParseSearchModifier(q.Filters.Modifier).SiteHost
	if siteHost == "" {
		return "", nil
	}
	hash, err := yacymodel.HashURLHost(siteHost)
	if err != nil {
		return "", fmt.Errorf("join site hash: %w", err)
	}
	hostHash, err := hash.HostHash()
	if err != nil {
		return "", fmt.Errorf("join site hash: %w", err)
	}
	return hostHash, nil
}
