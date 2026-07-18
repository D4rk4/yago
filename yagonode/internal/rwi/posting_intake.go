package rwi

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type postingIntake struct {
	vault             *vault.Vault
	postings          *vault.Collection[yagomodel.RWIPosting]
	observers         postingObservers
	urls              urlmeta.URLDirectory
	pauseMilliseconds int
}

func (i postingIntake) Receive(
	ctx context.Context,
	entries []yagomodel.RWIPosting,
) (Receipt, error) {
	if len(entries) == 0 {
		return Receipt{}, nil
	}
	busy, err := i.storageAtCapacity(ctx)
	if err != nil {
		return Receipt{}, err
	}
	if busy {
		return i.busyReceipt(), nil
	}

	referenced, busy, err := i.storeEntries(ctx, entries)
	if err != nil {
		return Receipt{}, err
	}
	if busy {
		return i.busyReceipt(), nil
	}

	unknown, err := i.urls.MissingURLs(ctx, referenced)
	if err != nil {
		return Receipt{}, fmt.Errorf("missing urls: %w", err)
	}

	return Receipt{UnknownURL: unknown}, nil
}
