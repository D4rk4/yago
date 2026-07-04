package urlmeta

import (
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const bucketName vault.Name = "urlmeta"

type uriMetadataCodec struct{}

func (uriMetadataCodec) Encode(row yagomodel.URIMetadataRow) ([]byte, error) {
	return encodeStoredURLMetadata(row), nil
}

func (uriMetadataCodec) Decode(raw []byte) (yagomodel.URIMetadataRow, error) {
	row, err := decodeStoredURLMetadata(raw)
	if err != nil {
		return yagomodel.URIMetadataRow{}, fmt.Errorf("decode url metadata: %w", err)
	}

	return row, nil
}

func registerCollection(
	v *vault.Vault,
) (*vault.Collection[yagomodel.URIMetadataRow], error) {
	collection, err := vault.Register(v, bucketName, uriMetadataCodec{})
	if err != nil {
		return nil, fmt.Errorf("register url metadata collection: %w", err)
	}

	return collection, nil
}
