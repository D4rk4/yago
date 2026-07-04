package rwi

import (
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	postingsBucket         vault.Name = "rwi"
	outboundSelectedBucket vault.Name = "rwi_outbound_selected"
)

const postingKeyLength = yagomodel.HashLength + yagomodel.HashLength

type postingCodec struct{}

func (postingCodec) Encode(entry yagomodel.RWIPosting) ([]byte, error) {
	return encodeStoredPosting(entry), nil
}

func (postingCodec) Decode(raw []byte) (yagomodel.RWIPosting, error) {
	entry, err := decodeStoredPosting("", raw)
	if err != nil {
		return yagomodel.RWIPosting{}, fmt.Errorf("decode rwi posting: %w", err)
	}

	return entry, nil
}

func registerPostings(
	v *vault.Vault,
) (*vault.Collection[yagomodel.RWIPosting], error) {
	collection, err := vault.Register(v, postingsBucket, postingCodec{})
	if err != nil {
		return nil, fmt.Errorf("register rwi posting collection: %w", err)
	}

	return collection, nil
}

func registerOutboundSelectedPostings(
	v *vault.Vault,
) (*vault.Collection[yagomodel.RWIPosting], error) {
	collection, err := vault.Register(v, outboundSelectedBucket, postingCodec{})
	if err != nil {
		return nil, fmt.Errorf("register outbound selected rwi collection: %w", err)
	}

	return collection, nil
}

func postingKey(wordHash, urlHash yagomodel.Hash) vault.Key {
	key := make(vault.Key, 0, postingKeyLength)
	key = append(key, wordHash.String()...)
	key = append(key, urlHash.String()...)

	return key
}
