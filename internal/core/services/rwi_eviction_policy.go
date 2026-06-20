package services

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type EvictionPolicy interface {
	Evict(ctx context.Context, candidates []yacymodel.Hash) (ports.EvictionResult, error)
}

type DropEvictionPolicy struct {
	evictor ports.RWIEvictor
}

func NewDropEvictionPolicy(evictor ports.RWIEvictor) DropEvictionPolicy {
	return DropEvictionPolicy{evictor: evictor}
}

func (p DropEvictionPolicy) Evict(
	ctx context.Context,
	candidates []yacymodel.Hash,
) (ports.EvictionResult, error) {
	result, err := p.evictor.DeleteURLs(ctx, candidates)
	if err != nil {
		return ports.EvictionResult{}, fmt.Errorf("drop candidates: %w", err)
	}

	return result, nil
}

var _ EvictionPolicy = DropEvictionPolicy{}
