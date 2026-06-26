package urlreferences

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

const (
	wordsByURLBucket    vault.Name = "urlreferences_words"
	referencedURLBucket vault.Name = "rwi_refs"
)

type urlReferences struct {
	vault      *vault.Vault
	words      *vault.Collection[struct{}]
	referenced *vault.Collection[struct{}]
}

func openURLReferences(v *vault.Vault) (*urlReferences, error) {
	words, err := vault.Register(v, wordsByURLBucket, presenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register words by url: %w", err)
	}
	referenced, err := vault.Register(v, referencedURLBucket, presenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register referenced urls: %w", err)
	}

	return &urlReferences{vault: v, words: words, referenced: referenced}, nil
}

func (r *urlReferences) PostingStored(tx *vault.Txn, word, url yacymodel.Hash) error {
	if err := r.words.Put(tx, wordByURL{url: url, word: word}.key(), struct{}{}); err != nil {
		return fmt.Errorf("record word by url: %w", err)
	}
	if err := r.referenced.Put(tx, vault.Key(url), struct{}{}); err != nil {
		return fmt.Errorf("record referenced url: %w", err)
	}

	return nil
}

func (r *urlReferences) PostingPurged(tx *vault.Txn, word, url yacymodel.Hash) error {
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
	if _, err := r.referenced.Delete(tx, vault.Key(url)); err != nil {
		return fmt.Errorf("drop referenced url: %w", err)
	}

	return nil
}

func (r *urlReferences) WordsReferencing(
	tx *vault.Txn,
	url yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	var words []yacymodel.Hash
	err := r.words.Scan(
		tx,
		vault.Key(url),
		func(key vault.Key, _ struct{}) (bool, error) {
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
	err := r.vault.View(ctx, func(tx *vault.Txn) error {
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
