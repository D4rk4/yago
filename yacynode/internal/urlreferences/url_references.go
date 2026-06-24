package urlreferences

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

const (
	wordsByURLBucket    boltvault.Name = "urlreferences_words"
	referencedURLBucket boltvault.Name = "rwi_refs"
)

type urlReferences struct {
	vault      *boltvault.Vault
	words      *boltvault.Collection[struct{}]
	referenced *boltvault.Collection[struct{}]
}

func openURLReferences(vault *boltvault.Vault) (*urlReferences, error) {
	words, err := boltvault.Register(vault, wordsByURLBucket, presenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register words by url: %w", err)
	}
	referenced, err := boltvault.Register(vault, referencedURLBucket, presenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register referenced urls: %w", err)
	}

	return &urlReferences{vault: vault, words: words, referenced: referenced}, nil
}

func (r *urlReferences) PostingStored(tx *boltvault.Txn, word, url yacymodel.Hash) error {
	if err := r.words.Put(tx, wordByURL{url: url, word: word}.key(), struct{}{}); err != nil {
		return fmt.Errorf("record word by url: %w", err)
	}
	if err := r.referenced.Put(tx, boltvault.Key(url), struct{}{}); err != nil {
		return fmt.Errorf("record referenced url: %w", err)
	}

	return nil
}

func (r *urlReferences) PostingPurged(tx *boltvault.Txn, word, url yacymodel.Hash) error {
	if _, err := r.words.Delete(tx, wordByURL{url: url, word: word}.key()); err != nil {
		return fmt.Errorf("drop word by url: %w", err)
	}

	remaining, err := r.WordsReferencing(tx, url)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return nil
	}
	if _, err := r.referenced.Delete(tx, boltvault.Key(url)); err != nil {
		return fmt.Errorf("drop referenced url: %w", err)
	}

	return nil
}

func (r *urlReferences) WordsReferencing(
	tx *boltvault.Txn,
	url yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	var words []yacymodel.Hash
	err := r.words.Scan(
		tx,
		boltvault.Key(url),
		func(key boltvault.Key, _ struct{}) (bool, error) {
			word, err := wordFromKey(key)
			if err != nil {
				return false, err
			}
			words = append(words, word)

			return true, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("scan words by url: %w", err)
	}

	return words, nil
}

func (r *urlReferences) ReferencedURLCount(ctx context.Context) (int, error) {
	var count int
	err := r.vault.View(ctx, func(tx *boltvault.Txn) error {
		measured, err := r.referenced.Len(tx)
		if err != nil {
			return fmt.Errorf("read referenced url count: %w", err)
		}
		count = measured

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("referenced url count: %w", err)
	}

	return count, nil
}

var _ ReferenceProjection = (*urlReferences)(nil)
