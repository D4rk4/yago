package services

import (
	"context"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

type fakeRWIStore struct {
	appended       [][]yacymodel.RWIEntry
	rejected       []yacymodel.Hash
	appendErr      error
	postings       map[yacymodel.Hash][]yacymodel.RWIEntry
	postingsErr    error
	postingsLimit  int
	postingsQuery  ports.PostingSearchQuery
	rwiCount       int
	rwiCountErr    error
	referencedURLs int
	referencedErr  error
}

func (s *fakeRWIStore) AppendRWI(
	_ context.Context,
	entries []yacymodel.RWIEntry,
) ([]yacymodel.Hash, error) {
	if s.appendErr != nil {
		return nil, s.appendErr
	}
	s.appended = append(s.appended, entries)

	return s.rejected, nil
}

func (s *fakeRWIStore) SearchPostings(
	_ context.Context,
	query ports.PostingSearchQuery,
) (ports.PostingSearchResult, error) {
	if s.postingsErr != nil {
		return ports.PostingSearchResult{}, s.postingsErr
	}
	s.postingsLimit = query.LimitPerWord
	s.postingsQuery = query

	excluded := fakeExcludedURLHashes(s.postings, query.ExcludeHashes)
	allowed := fakeHashSet(query.URLHashes)
	out := ports.PostingSearchResult{
		Postings: make(map[yacymodel.Hash][]yacymodel.RWIEntry, len(query.WordHashes)),
		Counts:   make(map[yacymodel.Hash]int, len(query.WordHashes)),
	}
	for _, word := range query.WordHashes {
		for _, entry := range s.postings[word] {
			if !fakePostingMatches(entry, query, allowed, excluded) {
				continue
			}
			out.Counts[word]++
			if query.LimitPerWord > 0 && len(out.Postings[word]) >= query.LimitPerWord {
				out.Truncated = true
				continue
			}
			out.Postings[word] = append(out.Postings[word], entry)
		}
	}

	return out, nil
}

func fakeExcludedURLHashes(
	postings map[yacymodel.Hash][]yacymodel.RWIEntry,
	words []yacymodel.Hash,
) map[yacymodel.Hash]struct{} {
	out := make(map[yacymodel.Hash]struct{})
	for _, word := range words {
		for _, entry := range postings[word] {
			if hash, err := entry.URLHash(); err == nil {
				out[hash] = struct{}{}
			}
		}
	}
	return out
}

func fakePostingMatches(
	entry yacymodel.RWIEntry,
	query ports.PostingSearchQuery,
	allowed map[yacymodel.Hash]struct{},
	excluded map[yacymodel.Hash]struct{},
) bool {
	if query.Language != "" && entry.Properties[yacymodel.ColLanguage] != query.Language {
		return false
	}
	distance, err := yacymodel.DecodeCardinal(entry.Properties[yacymodel.ColWordDistance])
	if err != nil {
		distance = 0
	}
	if query.MaxDistance > 0 && distance > uint64(query.MaxDistance) {
		return false
	}
	urlHash, err := entry.URLHash()
	if err != nil {
		return false
	}
	if len(allowed) != 0 {
		if _, ok := allowed[urlHash]; !ok {
			return false
		}
	}
	if _, ok := excluded[urlHash]; ok {
		return false
	}
	return true
}

func fakeHashSet(hashes []yacymodel.Hash) map[yacymodel.Hash]struct{} {
	if len(hashes) == 0 {
		return nil
	}
	out := make(map[yacymodel.Hash]struct{}, len(hashes))
	for _, hash := range hashes {
		out[hash] = struct{}{}
	}
	return out
}

func (s *fakeRWIStore) RWICount(_ context.Context) (int, error) {
	return s.rwiCount, s.rwiCountErr
}

func (s *fakeRWIStore) ReferencedURLCount(_ context.Context) (int, error) {
	return s.referencedURLs, s.referencedErr
}

type fakeURLStore struct {
	stored     [][]yacymodel.URIMetadataRow
	existing   []yacymodel.Hash
	rejected   []yacymodel.Hash
	storeErr   error
	missing    []yacymodel.Hash
	missingErr error
	rows       map[yacymodel.Hash]yacymodel.URIMetadataRow
	rowsErr    error
	urlCount   int
	countErr   error
}

func (s *fakeURLStore) MissingURLs(
	_ context.Context,
	_ []yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	return s.missing, s.missingErr
}

func (s *fakeURLStore) StoreURLs(
	_ context.Context,
	rows []yacymodel.URIMetadataRow,
) (ports.StoreURLsResult, error) {
	if s.storeErr != nil {
		return ports.StoreURLsResult{}, s.storeErr
	}
	s.stored = append(s.stored, rows)

	return ports.StoreURLsResult{Existing: s.existing, Rejected: s.rejected}, nil
}

func (s *fakeURLStore) RowsByHash(
	_ context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	if s.rowsErr != nil {
		return nil, s.rowsErr
	}
	out := make([]yacymodel.URIMetadataRow, 0, len(hashes))
	for _, h := range hashes {
		if row, ok := s.rows[h]; ok {
			out = append(out, row)
		}
	}

	return out, nil
}

func (s *fakeURLStore) URLCount(_ context.Context) (int, error) {
	return s.urlCount, s.countErr
}

func hashFor(base string) yacymodel.Hash {
	const filler = "AAAAAAAAAAAA"
	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + filler[len(base):])
}

func postingEntry(word yacymodel.Hash, url string, distance byte) yacymodel.RWIEntry {
	return yacymodel.RWIEntry{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash:      string(hashFor(url)),
			yacymodel.ColHitCount:     encodedCardinalForTest(1),
			yacymodel.ColWordDistance: encodedCardinalForTest(distance),
		},
	}
}

func encodedCardinalForTest(value byte) string {
	return yacymodel.Encode([]byte{value})
}

var (
	_ ports.Clock    = (*fakeClock)(nil)
	_ ports.RWIStore = (*fakeRWIStore)(nil)
	_ ports.URLStore = (*fakeURLStore)(nil)
)
