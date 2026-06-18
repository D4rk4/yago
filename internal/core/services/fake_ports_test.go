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

func (s *fakeRWIStore) PostingsForWords(
	_ context.Context,
	wordHashes []yacymodel.Hash,
	limitPerWord int,
) (map[yacymodel.Hash][]yacymodel.RWIEntry, error) {
	if s.postingsErr != nil {
		return nil, s.postingsErr
	}
	s.postingsLimit = limitPerWord

	out := make(map[yacymodel.Hash][]yacymodel.RWIEntry, len(wordHashes))
	for _, word := range wordHashes {
		entries := s.postings[word]
		if limitPerWord > 0 && len(entries) > limitPerWord {
			entries = entries[:limitPerWord]
		}
		out[word] = entries
	}

	return out, nil
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
