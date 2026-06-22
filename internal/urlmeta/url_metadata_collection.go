package urlmeta

import (
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const bucketName boltvault.Name = "urlmeta"

type uriMetadataCodec struct{}

func (uriMetadataCodec) Encode(row yacymodel.URIMetadataRow) ([]byte, error) {
	raw, err := yacymodel.EncodeURIMetadata(row)
	if err != nil {
		return nil, fmt.Errorf("encode url metadata: %w", err)
	}

	return raw, nil
}

func (uriMetadataCodec) Decode(raw []byte) (yacymodel.URIMetadataRow, error) {
	row, err := yacymodel.DecodeURIMetadata(raw)
	if err != nil {
		return yacymodel.URIMetadataRow{}, fmt.Errorf("decode url metadata: %w", err)
	}

	return row, nil
}

func registerCollection(
	vault *boltvault.Vault,
) (*boltvault.Collection[yacymodel.URIMetadataRow], error) {
	collection, err := boltvault.Register(vault, bucketName, uriMetadataCodec{})
	if err != nil {
		return nil, fmt.Errorf("register url metadata collection: %w", err)
	}

	return collection, nil
}
