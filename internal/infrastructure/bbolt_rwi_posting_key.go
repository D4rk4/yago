package infrastructure

import (
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const rwiPostingKeyLength = yacymodel.HashLength + yacymodel.HashLength

func rwiPostingKey(wordHash yacymodel.Hash, urlHash yacymodel.Hash) []byte {
	key := make([]byte, 0, rwiPostingKeyLength)
	key = append(key, wordHash.String()...)
	key = append(key, urlHash.String()...)

	return key
}

func parseRWIPostingKey(key []byte) (yacymodel.RWIPostingID, error) {
	if len(key) != rwiPostingKeyLength {
		return yacymodel.RWIPostingID{}, fmt.Errorf("rwi posting key length %d", len(key))
	}

	return yacymodel.RWIPostingID{
		WordHash: yacymodel.Hash(key[:yacymodel.HashLength]),
		URLHash:  yacymodel.Hash(key[yacymodel.HashLength:]),
	}, nil
}
