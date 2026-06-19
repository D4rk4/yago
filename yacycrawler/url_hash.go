package yacycrawler

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

func URLHash(rawURL string) yacymodel.Hash {
	return yacymodel.URLHash(rawURL)
}
