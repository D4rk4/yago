package infrastructure

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

func matchesSiteHash(urlHash yacymodel.Hash, siteHash string) bool {
	if siteHash == "" {
		return true
	}
	return urlHash.HostHash() == siteHash
}
