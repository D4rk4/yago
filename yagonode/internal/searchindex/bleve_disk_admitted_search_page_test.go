package searchindex

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type blockingSearchPageProbe struct {
	bleveIndexContract
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int64
	once    sync.Once
	result  *bleve.SearchResult
}

func (probe *blockingSearchPageProbe) SearchInContext(
	ctx context.Context,
	_ *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	call := probe.calls.Add(1)
	if call == 1 {
		probe.once.Do(func() { close(probe.entered) })
		select {
		case <-probe.release:
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for search page release: %w", context.Cause(ctx))
		}
	}

	return probe.result, nil
}

func TestBleveDiskSearchPageAdmissionBoundsNativeReads(t *testing.T) {
	probe := &blockingSearchPageProbe{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		result:  completeSearchPageProbeResult(nil),
	}
	index := &BleveDiskIndex{alias: probe, searchReadAdmission: make(chan struct{}, 1)}
	request := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	first := make(chan error, 1)
	go func() {
		_, err := index.readSearchPage(t.Context(), request)
		first <- err
	}()
	<-probe.entered
	waiting, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := index.readSearchPage(waiting, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting page error=%v", err)
	}
	if probe.calls.Load() != 1 {
		t.Fatalf("native search calls=%d, want=1", probe.calls.Load())
	}
	close(probe.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if _, err := index.readSearchPage(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if probe.calls.Load() != 2 || len(index.searchReadAdmission) != 0 {
		t.Fatalf(
			"native search calls/admission=%d/%d",
			probe.calls.Load(),
			len(index.searchReadAdmission),
		)
	}
}

type projectionReadBlock struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (block *projectionReadBlock) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, false, nil
}

func (*projectionReadBlock) Count(context.Context) (int, error) {
	return 1, nil
}

func (block *projectionReadBlock) DocumentExists(
	ctx context.Context,
	identity string,
) (bool, error) {
	found, err := block.DocumentsExist(ctx, []string{identity})
	if err != nil {
		return false, err
	}

	return found[0], nil
}

func (block *projectionReadBlock) DocumentsExist(
	ctx context.Context,
	identities []string,
) ([]bool, error) {
	block.once.Do(func() { close(block.entered) })
	select {
	case <-block.release:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for projection release: %w", context.Cause(ctx))
	}
	found := make([]bool, len(identities))
	for position := range found {
		found[position] = true
	}

	return found, nil
}

type projectionSearchPageProbe struct {
	bleveIndexContract
	result *bleve.SearchResult
	calls  atomic.Int64
}

func (*projectionSearchPageProbe) DocCount() (uint64, error) {
	return 1, nil
}

func (probe *projectionSearchPageProbe) SearchInContext(
	context.Context,
	*bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	probe.calls.Add(1)

	return probe.result, nil
}

func TestBleveDiskProjectionReadDoesNotRetainNativeReadAdmission(t *testing.T) {
	document := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		Title:         "Needle",
		ExtractedText: "needle evidence",
	}
	encoded, err := encodeStoredCandidateProjection(document)
	if err != nil {
		t.Fatal(err)
	}
	hit := &search.DocumentMatch{
		ID: document.NormalizedURL,
		Fields: map[string]any{
			storedCandidateField: encoded,
		},
	}
	probe := &projectionSearchPageProbe{result: completeSearchPageProbeResult(
		search.DocumentMatchCollection{hit},
	)}
	block := &projectionReadBlock{entered: make(chan struct{}), release: make(chan struct{})}
	index := &BleveDiskIndex{
		shards:              []bleve.Index{probe},
		alias:               probe,
		documents:           block,
		documentPresence:    block,
		storedCandidates:    true,
		searchReadAdmission: make(chan struct{}, 1),
	}
	first := make(chan error, 1)
	go func() {
		_, _, err := index.searchHits(t.Context(), SearchRequest{
			Query: "needle", Terms: []string{"needle"}, MaxResults: 1, CandidateOnly: true,
		})
		first <- err
	}()
	<-block.entered
	if _, err := index.readSearchPage(
		t.Context(),
		bleve.NewSearchRequest(bleve.NewMatchAllQuery()),
	); err != nil {
		t.Fatalf("second native read: %v", err)
	}
	if probe.calls.Load() != 2 {
		t.Fatalf("native search calls=%d, want=2", probe.calls.Load())
	}
	close(block.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func completeSearchPageProbeResult(hits search.DocumentMatchCollection) *bleve.SearchResult {
	return &bleve.SearchResult{
		Status: &bleve.SearchStatus{Total: 1, Successful: 1},
		Total:  uint64(len(hits)),
		Hits:   hits,
	}
}
