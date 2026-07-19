package rwi

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type StoredPosting struct {
	Cursor  []byte
	Posting yagomodel.RWIPosting
}

type StoredPostingPage struct {
	Entries []StoredPosting
	More    bool
}

type PostingPageSource interface {
	StoredPostingPage(context.Context, []byte, int) (StoredPostingPage, error)
}

func (d postingDirectory) StoredPostingPage(
	ctx context.Context,
	after []byte,
	limit int,
) (StoredPostingPage, error) {
	var result StoredPostingPage
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		page, err := tx.ReadBucketPage(PostingsBucket, vault.Key(after), limit)
		if err != nil {
			return fmt.Errorf("read posting bucket page: %w", err)
		}
		result.More = page.More
		result.Entries = make([]StoredPosting, 0, len(page.Entries))
		for _, entry := range page.Entries {
			if len(entry.Key) != postingKeyLength {
				return fmt.Errorf("stored posting key length %d", len(entry.Key))
			}
			word, err := yagomodel.ParseHash(string(entry.Key[:yagomodel.HashLength]))
			if err != nil {
				return fmt.Errorf("stored posting word: %w", err)
			}
			posting, err := decodeStoredPosting(word, entry.Value)
			if err != nil {
				return err
			}
			url, err := posting.URLHash()
			if err != nil {
				return fmt.Errorf("stored posting url: %w", err)
			}
			if url.String() != string(entry.Key[yagomodel.HashLength:]) {
				return fmt.Errorf("stored posting url does not match key")
			}
			result.Entries = append(result.Entries, StoredPosting{
				Cursor: append([]byte(nil), entry.Key...), Posting: posting,
			})
		}

		return nil
	})
	if err != nil {
		return StoredPostingPage{}, fmt.Errorf("read stored posting page: %w", err)
	}

	return result, nil
}

var _ PostingPageSource = postingDirectory{}
