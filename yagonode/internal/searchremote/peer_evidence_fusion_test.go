package searchremote

import (
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
	"github.com/D4rk4/yago/yagoproto"
)

func TestPeerFusionRetainsNegotiatedEvidenceAcrossPeerHashOrder(t *testing.T) {
	cases := []struct {
		name       string
		legacyPeer yagomodel.Hash
		yagoPeer   yagomodel.Hash
	}{
		{name: "legacy sorts first", legacyPeer: "000000000001", yagoPeer: "zzzzzzzzzzz1"},
		{name: "negotiated sorts first", legacyPeer: "zzzzzzzzzzz2", yagoPeer: "000000000002"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := fusedNegotiatedEvidenceResult(t, test.legacyPeer, test.yagoPeer)
			assertFusedNegotiatedEvidence(t, result)
		})
	}
}

func TestVariantFusionRetainsNegotiatedEvidenceAcrossSurfaceOrder(t *testing.T) {
	resource := hashFor("variant-duplicate")
	legacy := searchcore.Result{
		URLHash: resource.String(),
		URL:     "https://example.test/variant",
		Source:  searchcore.SourceRemote,
		Evidence: searchcore.NewRankingEvidence(searchcore.RankingSignalValue{
			Signal: searchcore.SignalURLScore,
			Value:  1,
		}),
	}
	negotiated := legacy
	negotiated.Analyzer = "ru"
	negotiated.EvidenceReady = true
	negotiated.EvidenceRequirementOrdinals = []int{0}
	negotiated.Snippet = "полномочий"
	negotiated.QueryMatches = []searchcore.QueryMatch{{Start: 0, End: len("полномочий")}}
	negotiated.BodyQueryMatches = []searchcore.QueryMatch{{Start: 40, End: 51}}
	negotiated.FieldTermPositions = map[string]map[string][]int{
		"body": {"полномочия": {12}},
	}
	negotiated.Evidence = searchcore.NewRankingEvidence(searchcore.RankingSignalValue{
		Signal: searchcore.SignalBodyScore,
		Value:  2,
	})
	for _, rankings := range [][][]searchcore.Result{
		{{legacy}, {negotiated}},
		{{negotiated}, {legacy}},
	} {
		fused := fuseRemoteVariantRankings(rankings)
		if len(fused) != 1 || !fused[0].EvidenceReady ||
			len(fused[0].BodyQueryMatches) != 1 {
			t.Fatalf("variant fusion = %#v", fused)
		}
		for signal, want := range map[searchcore.RankingSignal]float64{
			searchcore.SignalURLScore:  1,
			searchcore.SignalBodyScore: 2,
		} {
			if got, known := fused[0].Evidence.Value(signal); !known || got != want {
				t.Fatalf("signal %s = %v/%v, result = %#v", signal.Name(), got, known, fused[0])
			}
		}
	}
}

func TestRemotePayloadPreservesAuthoritativeEmptyBodyEvidence(t *testing.T) {
	legacy := searchcore.Result{URLHash: hashFor("empty-body").String()}
	authoritative := legacy
	authoritative.Analyzer = "en"
	authoritative.EvidenceReady = true
	authoritative.QueryMatches = []searchcore.QueryMatch{}
	authoritative.BodyQueryMatches = []searchcore.QueryMatch{}
	merged := mergedRemoteResultPayload(legacy, authoritative)
	if !merged.EvidenceReady || merged.BodyQueryMatches == nil ||
		merged.QueryMatches == nil || merged.FieldTermPositions != nil {
		t.Fatalf("merged payload = %#v", merged)
	}
}

func TestPeerFusionNegotiatedEvidenceSurvivesLaterSessionPage(t *testing.T) {
	retained := fusedNegotiatedEvidenceResult(t, "000000000003", "zzzzzzzzzzz3")
	results := make([]searchcore.Result, 20)
	for index := range results {
		results[index] = searchcore.Result{
			URL:   "https://page-" + strconv.Itoa(index) + ".example/result",
			Score: float64(100 - index),
		}
	}
	results[15] = retained
	source := &remoteEvidenceSessionSource{results: results}
	stable := searchsession.NewStableWindow(searchcore.NewFinalRankingSearcher(source))
	request := searchcore.Request{
		Query: "чрезвычайные полномочия",
		Terms: []string{"чрезвычайные", "полномочия"},
		Limit: 10,
	}
	first, err := stable.Search(t.Context(), request)
	if err != nil || len(first.Results) != 10 {
		t.Fatalf("first page = %#v, error = %v", first, err)
	}
	source.results[15].Snippet = "weaker mutation"
	source.results[15].EvidenceReady = false
	source.results[15].BodyQueryMatches[0].Start = 999
	request.Offset = 10
	second, err := stable.Search(t.Context(), request)
	if err != nil || len(second.Results) != 10 {
		t.Fatalf("second page = %#v, error = %v", second, err)
	}
	for _, result := range second.Results {
		if result.URLHash != retained.URLHash {
			continue
		}
		assertFusedNegotiatedEvidence(t, result)
		return
	}
	t.Fatalf("negotiated duplicate missing from second page: %#v", second.Results)
}

func fusedNegotiatedEvidenceResult(
	t *testing.T,
	legacyPeer yagomodel.Hash,
	yagoPeer yagomodel.Hash,
) searchcore.Result {
	t.Helper()
	resource := hashFor("fused-evidence")
	legacyRow := metadataRow(
		t,
		resource,
		"https://example.test/report",
		"Legacy metadata title",
	)
	yagoRow := metadataRow(
		t,
		resource,
		"https://example.test/report",
		"Negotiated metadata title",
	)
	requirements := []string{"чрезвычайные", "полномочия"}
	response := (searcher{weights: DefaultRankingWeights}).response(
		t.Context(),
		searchcore.Request{Terms: requirements, Limit: 10, Verify: searchcore.VerifyFalse},
		[]peerSearchResult{
			{
				peer: yagomodel.Seed{Hash: legacyPeer},
				response: yagoproto.SearchResponse{
					Count: 1, Resources: []yagomodel.URIMetadataRow{legacyRow},
				},
			},
			{
				peer: yagomodel.Seed{Hash: yagoPeer},
				response: yagoproto.SearchResponse{
					Count:            1,
					Resources:        []yagomodel.URIMetadataRow{yagoRow},
					ResourceEvidence: mapEvidence(resource, validRemoteQueryMatchEvidence()),
				},
				evidenceBinding: identityQueryMatchEvidenceBinding(requirements),
			},
		},
		nil,
	)
	if len(response.Results) != 1 {
		t.Fatalf("response = %#v", response)
	}

	return response.Results[0]
}

func assertFusedNegotiatedEvidence(t *testing.T, result searchcore.Result) {
	t.Helper()
	evidence := validRemoteQueryMatchEvidence()
	if !result.EvidenceReady || result.Analyzer != evidence.Analyzer ||
		result.Snippet != evidence.Snippet || len(result.QueryMatches) != 2 ||
		len(result.BodyQueryMatches) != 2 ||
		result.BodyQueryMatches[0].Start != evidence.BodyMatches[0].Start {
		t.Fatalf("negotiated payload = %#v", result)
	}
	if support, known := result.Evidence.Value(
		searchcore.SignalPeerSupport,
	); !known ||
		support != 2 {
		t.Fatalf("peer support = %v/%v, result = %#v", support, known, result)
	}
	if sources, known := result.Evidence.Value(
		searchcore.SignalSourceCount,
	); !known ||
		sources != 2 {
		t.Fatalf("source evidence = %v/%v, result = %#v", sources, known, result)
	}
}
