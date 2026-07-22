package rwi

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type OutboundSelectionConfig struct {
	MaxWords    int
	MaxPostings int
}

type OutboundSelection struct {
	Words []yagomodel.WordPostings
}

type OutboundPostingStore interface {
	SelectOutbound(ctx context.Context, config OutboundSelectionConfig) (OutboundSelection, error)
	RestoreOutbound(ctx context.Context, words []yagomodel.WordPostings) (int, error)
	ConfirmOutbound(ctx context.Context, postings []yagomodel.RWIPosting) (int, error)
	RecoverOutbound(ctx context.Context) (int, error)
}

type selectedPosting struct {
	key  vault.Key
	word yagomodel.Hash
	url  yagomodel.Hash
	row  yagomodel.RWIPosting
}

type outboundSelector struct {
	config    OutboundSelectionConfig
	selected  []selectedPosting
	seenWords map[yagomodel.Hash]struct{}
}

func newOutboundSelector(config OutboundSelectionConfig) *outboundSelector {
	return &outboundSelector{
		config:    config,
		selected:  make([]selectedPosting, 0, config.MaxPostings),
		seenWords: make(map[yagomodel.Hash]struct{}),
	}
}

func (s OutboundSelection) PostingCount() int {
	count := 0
	for _, word := range s.Words {
		count += len(word.Postings)
	}

	return count
}

func (d postingDirectory) SelectOutbound(
	ctx context.Context,
	config OutboundSelectionConfig,
) (OutboundSelection, error) {
	config = normalizeOutboundSelectionConfig(config)
	selected, err := d.selectOutboundPostings(ctx, config)
	if err != nil {
		return OutboundSelection{}, err
	}

	words := make([]yagomodel.WordPostings, 0, len(selected))
	positions := make(map[yagomodel.Hash]int)
	for _, posting := range selected {
		position, ok := positions[posting.word]
		if !ok {
			position = len(words)
			positions[posting.word] = position
			words = append(words, yagomodel.WordPostings{WordHash: posting.word})
		}
		words[position].Postings = append(words[position].Postings, posting.row)
	}

	return OutboundSelection{Words: words}, nil
}

func (d postingDirectory) RestoreOutbound(
	ctx context.Context,
	words []yagomodel.WordPostings,
) (int, error) {
	selected, err := outboundPostingsFromWords(words)
	if err != nil {
		return 0, fmt.Errorf("restore outbound rwi: %w", err)
	}
	restored, err := d.restoreOutboundPostings(ctx, selected)
	if err != nil {
		return 0, fmt.Errorf("restore outbound rwi: %w", err)
	}

	return restored, nil
}

func (d postingDirectory) restoreOutboundPosting(
	ctx context.Context,
	tx *vault.Txn,
	posting selectedPosting,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	posting.row.WordHash = posting.word
	if err := d.postings.Put(tx, posting.key, posting.row); err != nil {
		return fmt.Errorf("restore rwi posting: %w", err)
	}
	if err := d.observers.stored(tx, posting.word, posting.url); err != nil {
		return fmt.Errorf("observe outbound rwi restore: %w", err)
	}

	return nil
}

func (d postingDirectory) selectOutboundPostings(
	ctx context.Context,
	config OutboundSelectionConfig,
) ([]selectedPosting, error) {
	var selected []selectedPosting
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		selector := newOutboundSelector(config)
		if err := d.postings.Scan(tx, nil, func(
			key vault.Key,
			entry yagomodel.RWIPosting,
		) (bool, error) {
			return selector.visit(ctx, key, entry)
		}); err != nil {
			return fmt.Errorf("scan outbound rwi postings: %w", err)
		}
		if err := d.journalOutboundSelection(tx, selector.selected); err != nil {
			return err
		}
		selected = append(selected[:0], selector.selected...)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("select outbound rwi: %w", err)
	}
	if err := d.removeDurablySelectedPostings(ctx, selected); err != nil {
		return nil, fmt.Errorf("select outbound rwi: %w", err)
	}

	return selected, nil
}

func (s *outboundSelector) visit(
	ctx context.Context,
	key vault.Key,
	entry yagomodel.RWIPosting,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("context: %w", err)
	}
	word, url, err := postingKeyHashes(key)
	if err != nil {
		return false, err
	}
	if !s.keepWord(word) {
		return false, nil
	}
	entry.WordHash = word
	s.selected = append(s.selected, selectedPosting{key: key, word: word, url: url, row: entry})

	return len(s.selected) < s.config.MaxPostings, nil
}

func (s *outboundSelector) keepWord(word yagomodel.Hash) bool {
	if _, ok := s.seenWords[word]; ok {
		return true
	}
	if len(s.seenWords) >= s.config.MaxWords {
		return false
	}
	s.seenWords[word] = struct{}{}

	return true
}

func (d postingDirectory) deleteOutboundSelection(
	tx *vault.Txn,
	selected []selectedPosting,
) error {
	for _, posting := range selected {
		if err := d.deleteOutboundPosting(tx, posting); err != nil {
			return err
		}
	}

	return nil
}

func (d postingDirectory) deleteOutboundPosting(
	tx *vault.Txn,
	posting selectedPosting,
) error {
	if _, err := d.postings.Delete(tx, posting.key); err != nil {
		return fmt.Errorf("delete outbound rwi posting: %w", err)
	}
	if err := d.observers.purged(tx, posting.word, posting.url); err != nil {
		return fmt.Errorf("observe outbound rwi purge: %w", err)
	}

	return nil
}

func normalizeOutboundSelectionConfig(config OutboundSelectionConfig) OutboundSelectionConfig {
	if config.MaxWords <= 0 {
		config.MaxWords = 1
	}
	if config.MaxPostings <= 0 {
		config.MaxPostings = 1
	}

	return config
}

func postingKeyHashes(key vault.Key) (yagomodel.Hash, yagomodel.Hash, error) {
	if len(key) != postingKeyLength {
		return "", "", fmt.Errorf("rwi posting key length %d, want %d", len(key), postingKeyLength)
	}
	word, err := yagomodel.ParseHash(string(key[:yagomodel.HashLength]))
	if err != nil {
		return "", "", fmt.Errorf("rwi posting word hash: %w", err)
	}
	url, err := yagomodel.ParseHash(string(key[yagomodel.HashLength:]))
	if err != nil {
		return "", "", fmt.Errorf("rwi posting url hash: %w", err)
	}

	return word, url, nil
}

var _ OutboundPostingStore = postingDirectory{}
