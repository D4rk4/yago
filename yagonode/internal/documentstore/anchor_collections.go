package documentstore

import (
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	inboundAnchorBucket  vault.Name = "document_inbound_anchors"
	outboundTargetBucket vault.Name = "document_outbound_targets"
)

type anchorSliceCodec[V any] struct{}

func (anchorSliceCodec[V]) Encode(value V) ([]byte, error) {
	raw, _ := json.Marshal(value)

	return raw, nil
}

func (anchorSliceCodec[V]) Decode(raw []byte) (V, error) {
	var value V
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("unmarshal anchor data: %w", err)
	}

	return value, nil
}

func registerAnchorCollections(
	v *vault.Vault,
) (*vault.Collection[[]AnchorText], *vault.Collection[[]string], error) {
	inbound, err := vault.Register(v, inboundAnchorBucket, anchorSliceCodec[[]AnchorText]{})
	if err != nil {
		return nil, nil, fmt.Errorf("register inbound anchors: %w", err)
	}
	outbound, err := vault.Register(v, outboundTargetBucket, anchorSliceCodec[[]string]{})
	if err != nil {
		return nil, nil, fmt.Errorf("register outbound targets: %w", err)
	}

	return inbound, outbound, nil
}
