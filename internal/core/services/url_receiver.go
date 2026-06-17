package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type URLReceiver struct {
	store ports.URLStore
}

func NewURLReceiver(store ports.URLStore) URLReceiver {
	return URLReceiver{store: store}
}

func (r URLReceiver) ReceiveURLs(
	ctx context.Context,
	rows []yacymodel.URIMetadataRow,
) (contracts.URLReceipt, error) {
	result, err := r.store.StoreURLs(ctx, rows)
	if errors.Is(err, ports.ErrAtCapacity) {
		return contracts.URLReceipt{Busy: true}, nil
	}
	if err != nil {
		return contracts.URLReceipt{}, fmt.Errorf("store urls: %w", err)
	}

	return contracts.URLReceipt{Double: len(result.Existing), ErrorURL: result.Rejected}, nil
}
