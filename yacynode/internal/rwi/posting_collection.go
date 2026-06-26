package rwi

import (
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

const postingsBucket vault.Name = "rwi"

const postingKeyLength = yacymodel.HashLength + yacymodel.HashLength

type postingCodec struct{}

func (postingCodec) Encode(entry yacymodel.RWIPosting) ([]byte, error) {
	return yacymodel.EncodeRWIPosting(entry), nil
}

func (postingCodec) Decode(raw []byte) (yacymodel.RWIPosting, error) {
	entry, err := yacymodel.DecodeRWIPosting("", raw)
	if err != nil {
		return yacymodel.RWIPosting{}, fmt.Errorf("decode rwi posting: %w", err)
	}

	return entry, nil
}

func registerPostings(
	v *vault.Vault,
) (*vault.Collection[yacymodel.RWIPosting], error) {
	collection, err := vault.Register(v, postingsBucket, postingCodec{})
	if err != nil {
		return nil, fmt.Errorf("register rwi posting collection: %w", err)
	}

	return collection, nil
}

func postingKey(wordHash, urlHash yacymodel.Hash) vault.Key {
	key := make(vault.Key, 0, postingKeyLength)
	key = append(key, wordHash.String()...)
	key = append(key, urlHash.String()...)

	return key
}
