package infrastructure

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

func matchesSiteHash(urlHash yacymodel.URLHash, siteHash string) bool {
	if siteHash == "" {
		return true
	}
	hostHash, err := urlHash.HostHash()
	return err == nil && hostHash == siteHash
}
