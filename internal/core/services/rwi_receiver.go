package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type RWIReceiver struct {
	store     ports.RWIStore
	batchCap  int
	pauseSecs int
}

func NewRWIReceiver(store ports.RWIStore, batchCap, pauseSecs int) RWIReceiver {
	return RWIReceiver{store: store, batchCap: batchCap, pauseSecs: pauseSecs}
}

func (r RWIReceiver) ReceiveRWI(
	ctx context.Context,
	entries []yacymodel.RWIEntry,
) (contracts.RWIReceipt, error) {
	if len(entries) > r.batchCap {
		return contracts.RWIReceipt{Busy: true, Pause: r.pauseSecs}, nil
	}

	result, err := r.store.AppendRWI(ctx, entries)
	if errors.Is(err, ports.ErrAtCapacity) {
		return contracts.RWIReceipt{Busy: true, Pause: r.pauseSecs}, nil
	}
	if err != nil {
		return contracts.RWIReceipt{}, fmt.Errorf("append rwi: %w", err)
	}

	return contracts.RWIReceipt{
		UnknownURL: result.UnknownURLs,
		ErrorURL:   result.Rejected,
	}, nil
}
