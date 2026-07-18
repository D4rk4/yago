// Package transfertally keeps the running and persisted totals of this peer's
// YaCy index exchange for the advertised seed statistics.
package transfertally

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/vault"
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
	totals  *vault.Collection[int64]
	vault   *vault.Vault
	flush   sync.Mutex
	pending pendingTotals
}

type pendingTotals struct {
	sentWords     atomic.Int64
	receivedWords atomic.Int64
	sentURLs      atomic.Int64
	receivedURLs  atomic.Int64
}

type transferDeltas struct {
	sentWords     int64
	receivedWords int64
	sentURLs      int64
	receivedURLs  int64
}

type pendingTransferDelta struct {
	key     vault.Key
	delta   int64
	pending *atomic.Int64
}

func Open(v *vault.Vault) (*Tally, error) {
	totals, err := vault.Register(v, tallyBucket, totalCodec{})
	if err != nil {
		return nil, fmt.Errorf("register transfer tally: %w", err)
	}

	return &Tally{totals: totals, vault: v}, nil
}

func (t *Tally) AddSentWords(ctx context.Context, n int) error {
	return t.add(ctx, &t.pending.sentWords, n)
}

func (t *Tally) AddReceivedWords(ctx context.Context, n int) error {
	return t.add(ctx, &t.pending.receivedWords, n)
}

func (t *Tally) AddSentURLs(ctx context.Context, n int) error {
	return t.add(ctx, &t.pending.sentURLs, n)
}

func (t *Tally) AddReceivedURLs(ctx context.Context, n int) error {
	return t.add(ctx, &t.pending.receivedURLs, n)
}

func (t *Tally) add(ctx context.Context, pending *atomic.Int64, n int) error {
	if n <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("add transfer total: %w", err)
	}
	pending.Add(int64(n))

	return nil
}

func (t *Tally) Flush(ctx context.Context) error {
	t.flush.Lock()
	defer t.flush.Unlock()

	deltas := t.takePending()
	if deltas == (transferDeltas{}) {
		return nil
	}
	updates := []pendingTransferDelta{
		{sentWordsKey, deltas.sentWords, &t.pending.sentWords},
		{receivedWordsKey, deltas.receivedWords, &t.pending.receivedWords},
		{sentURLsKey, deltas.sentURLs, &t.pending.sentURLs},
		{receivedURLsKey, deltas.receivedURLs, &t.pending.receivedURLs},
	}
	for position, update := range updates {
		if update.delta == 0 {
			continue
		}
		if err := t.flushDelta(ctx, update); err != nil {
			restorePendingTransferDeltas(updates[position:])

			return fmt.Errorf("flush transfer totals: %w", err)
		}
	}

	return nil
}

func (t *Tally) flushDelta(ctx context.Context, update pendingTransferDelta) error {
	err := t.vault.Update(ctx, func(tx *vault.Txn) error {
		current, _, err := t.totals.Get(tx, update.key)
		if err != nil {
			return fmt.Errorf("read transfer total %s: %w", update.key, err)
		}
		if err := t.totals.Put(tx, update.key, current+update.delta); err != nil {
			return fmt.Errorf("store transfer total %s: %w", update.key, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("flush transfer total %s: %w", update.key, err)
	}

	return nil
}

func (t *Tally) Totals(ctx context.Context) (Totals, error) {
	t.flush.Lock()
	defer t.flush.Unlock()

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
	totals.SentWords += t.pending.sentWords.Load()
	totals.ReceivedWords += t.pending.receivedWords.Load()
	totals.SentURLs += t.pending.sentURLs.Load()
	totals.ReceivedURLs += t.pending.receivedURLs.Load()

	return totals, nil
}

func (t *Tally) takePending() transferDeltas {
	return transferDeltas{
		sentWords:     t.pending.sentWords.Swap(0),
		receivedWords: t.pending.receivedWords.Swap(0),
		sentURLs:      t.pending.sentURLs.Swap(0),
		receivedURLs:  t.pending.receivedURLs.Swap(0),
	}
}

func restorePendingTransferDeltas(updates []pendingTransferDelta) {
	for _, update := range updates {
		update.pending.Add(update.delta)
	}
}
