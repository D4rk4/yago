package urlreferences

import (
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

type wordByURL struct {
	url  yacymodel.Hash
	word yacymodel.Hash
}

func (w wordByURL) key() boltvault.Key {
	key := make(boltvault.Key, 0, 2*yacymodel.HashLength)
	key = append(key, w.url.String()...)
	key = append(key, w.word.String()...)

	return key
}

func wordFromKey(key boltvault.Key) (yacymodel.Hash, error) {
	if len(key) != 2*yacymodel.HashLength {
		return "", fmt.Errorf("word by url key length %d", len(key))
	}

	return yacymodel.Hash(key[yacymodel.HashLength:]), nil
}
