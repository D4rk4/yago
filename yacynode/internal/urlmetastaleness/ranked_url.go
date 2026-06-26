package urlmetastaleness

import (
	"bytes"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

const freshnessHashSeparator = 0x00

type rankedURL struct {
	freshness string
	hash      yacymodel.Hash
}

func (r rankedURL) orderKey() vault.Key {
	var key bytes.Buffer
	key.WriteString(r.freshness)
	key.WriteByte(freshnessHashSeparator)
	key.WriteString(string(r.hash))

	return key.Bytes()
}

func hashFromOrderKey(key vault.Key) (yacymodel.Hash, error) {
	_, encodedHash, found := bytes.Cut(key, []byte{freshnessHashSeparator})
	if !found {
		return "", fmt.Errorf("staleness order key without separator")
	}

	hash, err := yacymodel.ParseHash(string(encodedHash))
	if err != nil {
		return "", fmt.Errorf("staleness order hash: %w", err)
	}

	return hash, nil
}
