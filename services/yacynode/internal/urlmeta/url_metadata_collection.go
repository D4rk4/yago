package urlmeta

import (
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

const bucketName vault.Name = "urlmeta"

type uriMetadataCodec struct{}

func (uriMetadataCodec) Encode(row yacymodel.URIMetadataRow) ([]byte, error) {
	raw, err := encodeStoredURLMetadata(row)
	if err != nil {
		return nil, fmt.Errorf("encode url metadata: %w", err)
	}

	return raw, nil
}

func (uriMetadataCodec) Decode(raw []byte) (yacymodel.URIMetadataRow, error) {
	row, err := decodeStoredURLMetadata(raw)
	if err != nil {
		return yacymodel.URIMetadataRow{}, fmt.Errorf("decode url metadata: %w", err)
	}

	return row, nil
}

func registerCollection(
	v *vault.Vault,
) (*vault.Collection[yacymodel.URIMetadataRow], error) {
	collection, err := vault.Register(v, bucketName, uriMetadataCodec{})
	if err != nil {
		return nil, fmt.Errorf("register url metadata collection: %w", err)
	}

	return collection, nil
}
