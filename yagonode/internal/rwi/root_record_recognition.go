package rwi

import (
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func RecognizesPosting(key vault.Key, raw []byte) bool {
	word, url, err := postingKeyHashes(key)
	if err != nil {
		return false
	}
	posting, err := decodeStoredPosting(word, raw)
	if err != nil {
		return false
	}
	postingURL, err := posting.URLHash()

	return err == nil && postingURL.Hash() == url
}
