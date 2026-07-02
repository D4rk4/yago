// Package transfertally keeps the running totals of this peer's YaCy index
// exchange: words and URLs sent to peers and received from peers. The totals
// survive restarts and feed the sI, rI, sU, and rU statistics of the
// advertised seed.
package transfertally

import (
	"context"
	"fmt"
	"strconv"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const tallyBucket vault.Name = "transfertally"

var ErrBadTransferTally = fmt.Errorf("bad transfer tally")

type Totals struct {
	SentWords     int64
	ReceivedWords int64
	SentURLs      int64
	ReceivedURLs  int64
}

var (
	sentWordsKey     = vault.Key("sentWords")
	receivedWordsKey = vault.Key("receivedWords")
	sentURLsKey      = vault.Key("sentURLs")
	receivedURLsKey  = vault.Key("receivedURLs")
)

type totalCodec struct{}

func (totalCodec) Encode(value int64) ([]byte, error) {
	return []byte(strconv.FormatInt(value, 10)), nil
}

func (totalCodec) Decode(raw []byte) (int64, error) {
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%w: total %q", ErrBadTransferTally, raw)
	}

	return value, nil
}

type Tally struct {
	totals *vault.Collection[int64]
	vault  *vault.Vault
}

func Open(v *vault.Vault) (*Tally, error) {
	totals, err := vault.Register(v, tallyBucket, totalCodec{})
	if err != nil {
		return nil, fmt.Errorf("register transfer tally: %w", err)
	}

	return &Tally{totals: totals, vault: v}, nil
}

func (t *Tally) AddSentWords(ctx context.Context, n int) error {
	return t.add(ctx, sentWordsKey, n)
}

func (t *Tally) AddReceivedWords(ctx context.Context, n int) error {
	return t.add(ctx, receivedWordsKey, n)
}

func (t *Tally) AddSentURLs(ctx context.Context, n int) error {
	return t.add(ctx, sentURLsKey, n)
}

func (t *Tally) AddReceivedURLs(ctx context.Context, n int) error {
	return t.add(ctx, receivedURLsKey, n)
}

func (t *Tally) add(ctx context.Context, key vault.Key, n int) error {
	if n <= 0 {
		return nil
	}

	err := t.vault.Update(ctx, func(tx *vault.Txn) error {
		current, _, err := t.totals.Get(tx, key)
		if err != nil {
			return fmt.Errorf("read transfer total %s: %w", key, err)
		}
		if err := t.totals.Put(tx, key, current+int64(n)); err != nil {
			return fmt.Errorf("store transfer total %s: %w", key, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("add transfer total: %w", err)
	}

	return nil
}

func (t *Tally) Totals(ctx context.Context) (Totals, error) {
	var totals Totals
	err := t.vault.View(ctx, func(tx *vault.Txn) error {
		for field, storageKey := range map[*int64]vault.Key{
			&totals.SentWords:     sentWordsKey,
			&totals.ReceivedWords: receivedWordsKey,
			&totals.SentURLs:      sentURLsKey,
			&totals.ReceivedURLs:  receivedURLsKey,
		} {
			value, _, err := t.totals.Get(tx, storageKey)
			if err != nil {
				return fmt.Errorf("read transfer total %s: %w", storageKey, err)
			}
			*field = value
		}

		return nil
	})
	if err != nil {
		return Totals{}, fmt.Errorf("transfer totals: %w", err)
	}

	return totals, nil
}
