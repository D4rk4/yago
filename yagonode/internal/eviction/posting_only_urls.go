package eviction

import (
	"context"
	"fmt"
	"sync"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

const maximumPostingOnlyInspection = 4096

type PostingOnlyURLSource interface {
	PostingOnlyURLs(context.Context, int) ([]yagomodel.Hash, error)
}

type postingOnlyURLSource struct {
	mu     sync.Mutex
	pages  rwi.PostingPageSource
	urls   urlmeta.URLDirectory
	cursor []byte
}

func NewPostingOnlyURLSource(
	pages rwi.PostingPageSource,
	urls urlmeta.URLDirectory,
) PostingOnlyURLSource {
	if pages == nil || urls == nil {
		return nil
	}

	return &postingOnlyURLSource{pages: pages, urls: urls}
}

func (s *postingOnlyURLSource) PostingOnlyURLs(
	ctx context.Context,
	limit int,
) ([]yagomodel.Hash, error) {
	if limit <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	inspected := 0
	for inspected < maximumPostingOnlyInspection {
		pageLimit := min(limit, maximumPostingOnlyInspection-inspected)
		page, err := s.pages.StoredPostingPage(ctx, s.cursor, pageLimit)
		if err != nil {
			return nil, fmt.Errorf("scan posting-only urls: %w", err)
		}
		if len(page.Entries) == 0 {
			s.cursor = nil

			return nil, nil
		}
		inspected += len(page.Entries)
		s.cursor = append(s.cursor[:0], page.Entries[len(page.Entries)-1].Cursor...)
		candidates, err := postingURLCandidates(page.Entries)
		if err != nil {
			return nil, err
		}
		missing, err := s.urls.MissingURLs(ctx, candidates)
		if err != nil {
			return nil, fmt.Errorf("select posting-only urls: %w", err)
		}
		if !page.More {
			s.cursor = nil
		}
		if len(missing) > limit {
			missing = missing[:limit]
		}
		if len(missing) > 0 || !page.More {
			return missing, nil
		}
	}

	return nil, nil
}
