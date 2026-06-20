package contracts

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

func (q SearchQuery) JoinSiteHash() string {
	if q.Filters.SiteHash != "" {
		return q.Filters.SiteHash
	}
	return yacymodel.HostHashFromName(ParseSearchModifier(q.Filters.Modifier).SiteHost)
}
