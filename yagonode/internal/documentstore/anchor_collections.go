package documentstore

import (
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	inboundAnchorBucket             vault.Name = "document_inbound_anchors"
	outboundTargetBucket            vault.Name = "document_outbound_targets"
	outboundAnchorPublicationBucket vault.Name = "document_outbound_anchor_publications"
)

type anchorJSONCodec[V any] struct{}

func (anchorJSONCodec[V]) Encode(value V) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal anchor data: %w", err)
	}

	return raw, nil
}

func (anchorJSONCodec[V]) Decode(raw []byte) (V, error) {
	var value V
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("unmarshal anchor data: %w", err)
	}

	return value, nil
}

func registerAnchorCollections(
	v *vault.Vault,
) (
	*vault.Keyspace[[]AnchorText],
	*vault.Keyspace[[]string],
	*vault.Keyspace[outboundAnchorPublication],
	error,
) {
	inbound, err := vault.RegisterKeyspace(
		v,
		inboundAnchorBucket,
		anchorJSONCodec[[]AnchorText]{},
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("register inbound anchors: %w", err)
	}
	outbound, err := vault.RegisterKeyspace(
		v,
		outboundTargetBucket,
		anchorJSONCodec[[]string]{},
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("register outbound targets: %w", err)
	}
	publications, err := vault.RegisterKeyspace(
		v,
		outboundAnchorPublicationBucket,
		anchorJSONCodec[outboundAnchorPublication]{},
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("register outbound anchor publications: %w", err)
	}

	return inbound, outbound, publications, nil
}
