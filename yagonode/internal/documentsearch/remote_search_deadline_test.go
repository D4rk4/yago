package documentsearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type deadlinePostingIndex struct{}

func (deadlinePostingIndex) RWICount(context.Context) (int, error) { return 1, nil }

func (deadlinePostingIndex) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return 1, nil
}

func (deadlinePostingIndex) ScanWord(
	ctx context.Context,
	word yagomodel.Hash,
	visit func(yagomodel.RWIPosting) (bool, error),
) error {
	keepGoing, err := visit(postingEntry(word, "u1", 0, 1))
	if err != nil || !keepGoing {
		return err
	}
	<-ctx.Done()

	return fmt.Errorf("scan postings: %w", ctx.Err())
}

func TestEndpointOwnedDeadlineReturnsEmptyAndReleasesAdmission(t *testing.T) {
	gate := httpguard.NewIntakeGate(1)
	endpoint := searchEndpoint{
		identity: searchIdentity(),
		searcher: searcher{
			index:          deadlinePostingIndex{},
			documents:      fakeDirectory{rows: urlRows("u1")},
			matchesPerTerm: 100,
		},
		gate: gate,
	}
	word := hashFor("w1")
	resp, err := endpoint.Serve(t.Context(), yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yagomodel.Hash{word},
		Time:        5,
		Abstracts:   yagoproto.SearchAbstractsAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SearchTime < 5 {
		t.Fatalf("SearchTime = %d, want at least 5", resp.SearchTime)
	}
	if resp.Count != 0 || resp.JoinCount != 0 || len(resp.Resources) != 0 ||
		len(resp.IndexCount) != 0 || len(resp.IndexAbstract) != 0 {
		t.Fatalf("deadline response = %#v, want empty", resp)
	}
	release, admitted := gate.TryAcquire()
	if !admitted {
		t.Fatal("deadline response retained its admission slot")
	}
	release()
}

func TestCallerCancellationRemainsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	endpoint := searchEndpoint{
		identity: searchIdentity(),
		searcher: searcher{index: deadlinePostingIndex{}, documents: fakeDirectory{}},
	}
	_, err := endpoint.Serve(ctx, yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yagomodel.Hash{hashFor("w1")},
		Time:        100,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestCallerDeadlineRemainsAnError(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	endpoint := searchEndpoint{
		identity: searchIdentity(),
		searcher: searcher{index: deadlinePostingIndex{}, documents: fakeDirectory{}},
	}
	_, err := endpoint.Serve(ctx, yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yagomodel.Hash{hashFor("w1")},
		Time:        100,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestRemoteSearchDeadlineWireResponseIsPeerConsumable(t *testing.T) {
	mux := http.NewServeMux()
	MountSearch(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(searchWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		searchIdentity(),
		SearchConfig{
			Index:          deadlinePostingIndex{},
			Documents:      fakeDirectory{rows: urlRows("u1")},
			MatchesPerTerm: 100,
		},
	)
	word := hashFor("w1")
	req := yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yagomodel.Hash{word},
		Time:        5,
		Abstracts:   yagoproto.SearchAbstractsAuto,
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathSearch+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	parsed, err := yagoproto.ParseSearchResponse(mustParseMessage(t, rec.Body.String()))
	if err != nil {
		t.Fatalf("peer parser rejected deadline response: %v", err)
	}
	if parsed.SearchTime < 5 || parsed.Count != 0 || parsed.JoinCount != 0 ||
		len(parsed.Resources) != 0 || len(parsed.IndexCount) != 0 ||
		len(parsed.IndexAbstract) != 0 {
		t.Fatalf("deadline response = %#v, want parseable empty response", parsed)
	}
	if strings.Contains(rec.Body.String(), "indexcount.") ||
		strings.Contains(rec.Body.String(), "indexabstract.") {
		t.Fatalf("deadline response exposes partial index data: %s", rec.Body.String())
	}
}
