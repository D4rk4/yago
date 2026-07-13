package searchremote

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestQueryPeerJobsPreservesRequestOrderWhenLaterPeerFinishesFirst(t *testing.T) {
	laterPeerAnswered := make(chan struct{})
	firstServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			<-laterPeerAnswered
			time.Sleep(20 * time.Millisecond)
			writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
		}),
	)
	defer firstServer.Close()
	laterServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
			close(laterPeerAnswered)
		}),
	)
	defer laterServer.Close()

	firstPeer := serverSeed(t, firstServer.URL)
	laterPeer := serverSeed(t, laterServer.URL)
	remote := searcher{
		client:         firstServer.Client(),
		concurrency:    2,
		perPeerTimeout: time.Second,
	}
	got := remote.queryPeerJobs(t.Context(), []peerSearchJob{
		{peer: firstPeer},
		{peer: laterPeer},
	})
	if len(got) != 2 || got[0].peer.Hash != firstPeer.Hash || got[1].peer.Hash != laterPeer.Hash {
		t.Fatalf("peer completion order leaked into results: %#v", got)
	}
}

func TestPeerRankFusionIsStableAcrossDuplicatesTiesAndCompletionOrders(t *testing.T) {
	common := hashFor("Common")
	firstOnly := hashFor("AUnique")
	secondOnly := hashFor("BUnique")
	first := peerSearchResult{
		peer: yagomodel.Seed{Hash: hashFor("APeer")},
		response: yagoproto.SearchResponse{Resources: []yagomodel.URIMetadataRow{
			metadataRow(t, common, "https://common.example/result", "Common from A"),
			metadataRow(t, common, "https://common.example/result", "Duplicate from A"),
			metadataRow(t, firstOnly, "https://a.example/only", "Unique azure handbook"),
		}},
	}
	second := peerSearchResult{
		peer: yagomodel.Seed{Hash: hashFor("BPeer")},
		response: yagoproto.SearchResponse{Resources: []yagomodel.URIMetadataRow{
			metadataRow(t, common, "https://common.example/result", "Common from B"),
			metadataRow(t, secondOnly, "https://b.example/only", "Unique bronze manual"),
		}},
	}
	remote := searcher{weights: DefaultRankingWeights}
	req := searchcore.Request{Terms: []string{"query"}, Limit: 10, Verify: searchcore.VerifyFalse}

	var baseline searchcore.Response
	for iteration := range 100 {
		completed := []peerSearchResult{first, second}
		if iteration%2 == 0 {
			completed[0], completed[1] = completed[1], completed[0]
		}
		got := remote.response(t.Context(), req, completed, nil)
		if got.TotalResults != 3 || len(got.Results) != 3 {
			t.Fatalf("iteration %d result count = %#v", iteration, got)
		}
		if got.Results[0].URLHash != common.String() || got.Results[0].Title != "Common from A" {
			t.Fatalf("iteration %d duplicate fusion = %#v", iteration, got.Results)
		}
		if got.Results[1].URLHash != firstOnly.String() ||
			got.Results[2].URLHash != secondOnly.String() {
			t.Fatalf("iteration %d tie order = %#v", iteration, got.Results)
		}
		if got.Results[0].Score != 2.0/61.0 || got.Results[1].Score != 1.0/62.0 ||
			got.Results[2].Score != 1.0/62.0 {
			t.Fatalf("iteration %d fused scores = %#v", iteration, got.Results)
		}
		if iteration == 0 {
			baseline = got
			continue
		}
		if !reflect.DeepEqual(got, baseline) {
			t.Fatalf(
				"iteration %d changed response:\nfirst: %#v\nnext:  %#v",
				iteration,
				baseline,
				got,
			)
		}
	}
}

func TestPeerSearchFailuresAndTimeoutsHaveStableOrder(t *testing.T) {
	timedOutPeer := searchSeed(t, "APeer")
	timedOutPeer.Port = yagomodel.Some(yagomodel.Port(8101))
	successfulPeer := searchSeed(t, "BPeer")
	successfulPeer.Port = yagomodel.Some(yagomodel.Port(8102))
	failedPeer := searchSeed(t, "CPeer")
	failedPeer.Port = yagomodel.Some(yagomodel.Port(8103))
	successBody := yagoproto.SearchResponse{Resources: []yagomodel.URIMetadataRow{
		metadataRow(t, hashFor("result"), "https://example.org/result", "Result"),
	}}.Encode().Encode()
	remote := searcher{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Port() {
				case "8101":
					<-req.Context().Done()
					return nil, req.Context().Err()
				case "8102":
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(successBody)),
						Header:     make(http.Header),
					}, nil
				default:
					return nil, errors.New("connection refused")
				}
			}),
		},
		concurrency:    3,
		perPeerTimeout: 10 * time.Millisecond,
		weights:        DefaultRankingWeights,
	}
	completed := remote.queryPeerJobs(t.Context(), []peerSearchJob{
		{peer: failedPeer},
		{peer: successfulPeer},
		{peer: timedOutPeer},
	})
	if len(completed) != 3 || completed[0].peer.Hash != failedPeer.Hash ||
		completed[1].peer.Hash != successfulPeer.Hash || completed[2].peer.Hash != timedOutPeer.Hash {
		t.Fatalf("request order = %#v", completed)
	}
	if !errors.Is(completed[2].err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", completed[2].err)
	}

	resp := remote.response(t.Context(), searchcore.Request{Limit: 10}, completed, nil)
	if len(resp.Results) != 1 || len(resp.PartialFailures) != 2 {
		t.Fatalf("response = %#v", resp)
	}
	if resp.PartialFailures[0].Source != timedOutPeer.Hash.String() ||
		!strings.Contains(resp.PartialFailures[0].Reason, context.DeadlineExceeded.Error()) ||
		resp.PartialFailures[1].Source != failedPeer.Hash.String() ||
		!strings.Contains(resp.PartialFailures[1].Reason, "connection refused") {
		t.Fatalf("partial failure order = %#v", resp.PartialFailures)
	}
}

func TestPeerResponseDoesNotTreatFreshnessAsPublicationDate(t *testing.T) {
	older := metadataRow(t, hashFor("older"), "https://example.org/older", "Older result")
	older.Properties[yagomodel.ColLoadDate] = "20200101"
	newer := metadataRow(t, hashFor("newer"), "https://example.org/newer", "Newer result")
	newer.Properties[yagomodel.ColLoadDate] = "20260101"
	remote := searcher{weights: DefaultRankingWeights}
	resp := remote.response(t.Context(), searchcore.Request{
		Limit: 10, SortByDate: true, Verify: searchcore.VerifyFalse,
	}, []peerSearchResult{{
		peer:     yagomodel.Seed{Hash: hashFor("peer")},
		response: yagoproto.SearchResponse{Resources: []yagomodel.URIMetadataRow{older, newer}},
	}}, nil)
	if len(resp.Results) != 2 ||
		resp.Results[0].URL != "https://example.org/older" ||
		resp.Results[0].Date != "" ||
		resp.Results[1].Date != "" {
		t.Fatalf("peer freshness became publication: %#v", resp.Results)
	}
}

func TestPeerResponseRanksMetadataEvidenceBeforeFusion(t *testing.T) {
	remote := searcher{weights: DefaultRankingWeights}
	peer := yagomodel.Seed{Hash: hashFor("peer")}
	resp := remote.response(t.Context(), searchcore.Request{
		Terms: []string{"golang"}, Limit: 10, Verify: searchcore.VerifyIfExist,
	}, []peerSearchResult{{
		peer: peer,
		response: yagoproto.SearchResponse{Resources: []yagomodel.URIMetadataRow{
			metadataRow(t, hashFor("first"), "https://example.org/first", "Unrelated"),
			metadataRow(t, hashFor("second"), "https://example.org/second", "Golang guide"),
		}},
	}}, nil)
	if len(resp.Results) != 2 || resp.Results[0].Title != "Golang guide" {
		t.Fatalf("metadata ranking = %#v", resp.Results)
	}
}

func TestPeerFusionCapsRepeatedRepliesFromOnePeer(t *testing.T) {
	peer := yagomodel.Seed{Hash: hashFor("peer")}
	row := metadataRow(t, hashFor("result"), "https://example.org/result", "Result")
	remote := searcher{weights: DefaultRankingWeights}
	resp := remote.response(t.Context(), searchcore.Request{
		Limit: 10, Verify: searchcore.VerifyFalse,
	}, []peerSearchResult{
		{
			peer:     peer,
			response: yagoproto.SearchResponse{Resources: []yagomodel.URIMetadataRow{row}},
		},
		{term: hashFor("term"), peer: peer, response: yagoproto.SearchResponse{
			Resources: []yagomodel.URIMetadataRow{row},
		}},
	}, nil)
	if len(resp.Results) != 1 || resp.Results[0].Score != 1.0/61.0 {
		t.Fatalf("repeated peer influence = %#v", resp.Results)
	}
}

func TestPeerRankingIdentityFallbacks(t *testing.T) {
	if got := peerRankingIdentity(
		yagomodel.Seed{Hash: hashFor("peer")},
	); !strings.HasPrefix(
		got,
		"hash:",
	) {
		t.Fatalf("hash identity = %q", got)
	}
	addressed := serverSeed(t, "http://127.0.0.1:8090")
	addressed.Hash = ""
	if got := peerRankingIdentity(addressed); got != "address:127.0.0.1:8090" {
		t.Fatalf("address identity = %q", got)
	}
	if got := peerRankingIdentity(yagomodel.Seed{}); !strings.HasPrefix(got, "seed:") {
		t.Fatalf("seed identity = %q", got)
	}
}
