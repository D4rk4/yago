package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type RWIReceiver struct {
	rwi         ports.RWIStore
	urls        ports.URLStore
	batchCap    int
	pauseSecs   int
	afterAppend []func()
}

type RWIReceiverOption func(*RWIReceiver)

func WithEvictionTrigger(trigger func()) RWIReceiverOption {
	return WithAfterAppend(trigger)
}

func WithAfterAppend(hook func()) RWIReceiverOption {
	return func(r *RWIReceiver) {
		if hook != nil {
			r.afterAppend = append(r.afterAppend, hook)
		}
	}
}

func NewRWIReceiver(
	rwi ports.RWIStore,
	urls ports.URLStore,
	batchCap, pauseSecs int,
	opts ...RWIReceiverOption,
) RWIReceiver {
	receiver := RWIReceiver{rwi: rwi, urls: urls, batchCap: batchCap, pauseSecs: pauseSecs}
	for _, opt := range opts {
		opt(&receiver)
	}

	return receiver
}

func (r RWIReceiver) ReceiveRWI(
	ctx context.Context,
	entries []yacymodel.RWIPosting,
) (contracts.RWIReceipt, error) {
	if len(entries) > r.batchCap {
		return contracts.RWIReceipt{Busy: true, Pause: r.pauseSecs}, nil
	}

	rejected, err := r.rwi.AppendRWI(ctx, entries)
	if errors.Is(err, ports.ErrAtCapacity) {
		return contracts.RWIReceipt{Busy: true, Pause: r.pauseSecs}, nil
	}
	if err != nil {
		return contracts.RWIReceipt{}, fmt.Errorf("append rwi: %w", err)
	}

	for _, hook := range r.afterAppend {
		hook()
	}

	unknown, err := r.urls.MissingURLs(ctx, referencedURLs(ctx, entries))
	if err != nil {
		return contracts.RWIReceipt{}, fmt.Errorf("missing urls: %w", err)
	}

	return contracts.RWIReceipt{UnknownURL: unknown, ErrorURL: rejected}, nil
}

func referencedURLs(ctx context.Context, entries []yacymodel.RWIPosting) []yacymodel.Hash {
	hashes := make([]yacymodel.Hash, 0, len(entries))
	for _, entry := range entries {
		hash, err := entry.URLHash()
		if err != nil {
			slog.WarnContext(
				ctx,
				"rwi reference discarded",
				slog.String("reason", "invalid url hash"),
				slog.Any("error", err),
			)
			continue
		}
		hashes = append(hashes, hash.Hash())
	}

	return hashes
}
