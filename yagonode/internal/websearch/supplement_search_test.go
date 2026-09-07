package websearch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSupplementThresholdUsesUniquePrimaryCandidates(t *testing.T) {
	for _, size := range []int{0, 1, 99, 100, 101} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			rows := make([]searchcore.Result, size)
			for ordinal := range rows {
				rows[ordinal] = searchcore.Result{
					URL: fmt.Sprintf(
						"https://primary.example/%d",
						ordinal,
					),
					Title: "Needle reference",
				}
			}
			primary := &stubSearcher{resp: searchcore.Response{Results: rows, TotalResults: 100000}}
			provider := &stubProvider{
				results: []Result{{URL: "https://web.example/new", Title: "Needle reference"}},
			}
			response, err := NewFallbackSearcher(
				primary,
				provider,
				enabled,
			).Search(t.Context(), searchcore.Request{Query: "needle", Limit: 1})
			if err != nil || len(response.Results) != 1 ||
				(provider.calls > 0) != (size < SupplementalCandidateTarget) {
				t.Fatalf("rows=%d calls=%d error=%v", len(response.Results), provider.calls, err)
			}
		})
	}
}

func TestSupplementRejectsDuplicateClusterFilteredAndUnsafeBreadth(t *testing.T) {
	request := searchcore.Request{
		Query:          "needle",
		ExcludeDomains: []string{"blocked.example"},
		SafeSearch:     true,
	}
	response, known := primaryCandidates(request, searchcore.Response{Results: []searchcore.Result{
		{URL: "https://PRIMARY.example:443/item#one", ClusterID: "cluster"},
		{URL: "https://primary.example/item#two"},
		{URL: "https://primary.example/another", ClusterID: "cluster"},
		{URL: "https://blocked.example/item"},
		{URL: "https://unsafe.example/item", SafetyRating: searchcore.SafetyExplicit},
		{URL: "https://second.example/item"},
	}})
	if len(response.Results) != 2 || len(known) != 3 {
		t.Fatalf("candidates=%+v", response.Results)
	}
}

func TestWebSupplementCompletesWhileRecoveryIsBlocked(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	recovery := func(ctx context.Context, _ searchcore.Request, response searchcore.Response) (searchcore.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return response, ctx.Err()
	}
	provider := &recoveryBarrierProvider{started: started}
	response, err := NewFallbackSearcher(
		&stubSearcher{},
		provider,
		enabled,
		WithRecovery(recovery),
	).Search(
		t.Context(), searchcore.Request{Query: "needle", Limit: 10},
	)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].Source != searchcore.SourceWeb {
		t.Fatalf("response=%+v error=%v", response, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("recovery was not canceled")
	}
}

type recoveryBarrierProvider struct{ started <-chan struct{} }

func (p *recoveryBarrierProvider) Search(context.Context, string, int) ([]Result, error) {
	<-p.started
	return []Result{{URL: "https://web.example/new", Title: "Needle reference"}}, nil
}
