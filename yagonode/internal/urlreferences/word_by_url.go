package urlreferences

import (
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type wordByURL struct {
	url  yagomodel.Hash
	word yagomodel.Hash
}

func (w wordByURL) key() vault.Key {
	key := make(vault.Key, 0, 2*yagomodel.HashLength)
	key = append(key, w.url.String()...)
	key = append(key, w.word.String()...)

	return key
}

func wordFromKey(key vault.Key) (yagomodel.Hash, error) {
	if len(key) != 2*yagomodel.HashLength {
		return "", fmt.Errorf("word by url key length %d", len(key))
	}

	return yagomodel.Hash(key[yagomodel.HashLength:]), nil
}
