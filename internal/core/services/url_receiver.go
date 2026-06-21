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
	store      ports.URLStore
	afterStore []func()
}

type URLReceiverOption func(*URLReceiver)

func WithURLEvictionTrigger(trigger func()) URLReceiverOption {
	return func(r *URLReceiver) {
		if trigger != nil {
			r.afterStore = append(r.afterStore, trigger)
		}
	}
}

func NewURLReceiver(store ports.URLStore, opts ...URLReceiverOption) URLReceiver {
	receiver := URLReceiver{store: store}
	for _, opt := range opts {
		opt(&receiver)
	}

	return receiver
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

	for _, hook := range r.afterStore {
		hook()
	}

	return contracts.URLReceipt{Double: len(result.Existing), ErrorURL: result.Rejected}, nil
}
