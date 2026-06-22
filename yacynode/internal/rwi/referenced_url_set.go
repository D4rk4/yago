package rwi

import (
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

const referencesBucket boltvault.Name = "rwi_refs"

type referenceCodec struct{}

func (referenceCodec) Encode(struct{}) ([]byte, error) { return []byte{1}, nil }

func (referenceCodec) Decode([]byte) (struct{}, error) { return struct{}{}, nil }

func registerReferences(vault *boltvault.Vault) (*boltvault.Collection[struct{}], error) {
	collection, err := boltvault.Register(vault, referencesBucket, referenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register referenced url set: %w", err)
	}

	return collection, nil
}
