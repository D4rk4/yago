package yacycrawler

import (
	"crypto/md5"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

// URLHash is provisional: it is NOT compatible with YaCy's Java
// MultiProtocolURL.hash() (host-hash + path-hash + protocol/port flags).
// It only yields a deterministic, valid 12-char hash so the prototype runs
// end to end. Replace with a YaCy-conformant implementation, verified against
// a real peer, before integrating with a live node.
func URLHash(rawURL string) yacymodel.Hash {
	sum := md5.Sum([]byte(rawURL))
	return yacymodel.Hash(yacymodel.Encode(sum[:])[:yacymodel.HashLength])
}
