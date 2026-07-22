package searchremote

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestWeightedPeerRankFusion(t *testing.T) {
	t.Parallel()
	first := searchcore.Result{URLHash: "first", Source: searchcore.SourceRemote}
	second := searchcore.Result{URLHash: "second", Source: searchcore.SourceRemote}
	fused := fuseWeightedPeerRankings(
		[][]searchcore.Result{{first}, {second}},
		[]float64{1.5, 0.5},
		[]float64{1.5, 0.5},
	)
	if len(fused) != 2 || fused[0].URLHash != "first" ||
		fused[0].Score != 1.5/61 || fused[1].Score != 0.5/61 {
		t.Fatalf("weighted fusion = %#v", fused)
	}
	support, found := fused[0].Evidence.Value(searchcore.SignalPeerSupport)
	if !found || support != 1.5 {
		t.Fatalf("weighted support = %v, %v", support, found)
	}
	reputation, found := fused[0].Evidence.Value(searchcore.SignalPeerReputation)
	if !found || reputation != 1.5 {
		t.Fatalf("weighted reputation = %v, %v", reputation, found)
	}
	shared := searchcore.Result{URLHash: "shared", Source: searchcore.SourceRemote}
	sharedFusion := fuseWeightedPeerRankings(
		[][]searchcore.Result{{shared}, {shared}},
		[]float64{1.5, 0.5},
		[]float64{1.5, 0.5},
	)
	sharedReputation, found := sharedFusion[0].Evidence.Value(
		searchcore.SignalPeerReputation,
	)
	if !found || sharedReputation != 1 || sharedFusion[0].Score != 2.0/61.0 {
		t.Fatalf("shared reputation = %#v", sharedFusion)
	}
	fallback := fuseWeightedPeerRankings(
		[][]searchcore.Result{{first}, {second}},
		[]float64{1},
		[]float64{1, 1},
	)
	if len(fallback) != 2 || fallback[0].Score != 1.0/61.0 {
		t.Fatalf("mismatched weights fallback = %#v", fallback)
	}
	neutral, found := fallback[0].Evidence.Value(searchcore.SignalPeerReputation)
	if !found || neutral != 1 {
		t.Fatalf("neutral reputation = %v, %v", neutral, found)
	}
}

func TestInvalidProtocolResponseOutcome(t *testing.T) {
	t.Parallel()
	err := errors.Join(errRemoteSearchFailed, errRemoteSearchInvalidResult)
	if observationOutcome(err, false) != peerreputation.OutcomeInvalidResult {
		t.Fatal("invalid protocol response was not penalized")
	}
}

func TestReputationFusionWeightsCapSharedNetworkGroup(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_800_000_000, 0).UTC()
	first := yagomodel.Seed{Hash: hashFor("APeer")}
	second := yagomodel.Seed{Hash: hashFor("BPeer")}
	snapshot := reputationSnapshot(t, at, map[yagomodel.Hash]float64{
		first.Hash:  1.5,
		second.Hash: 1,
	})
	session := reputationSession{
		snapshot:              snapshot,
		snapshotAvailable:     true,
		maximumGroupInfluence: 1.5,
		networkGroup: func(yagomodel.Seed) peerreputation.NetworkGroupKey {
			return "shared"
		},
	}
	firstRanking := peerRankingIdentity(first)
	secondRanking := peerRankingIdentity(second)
	influenceWeights, reputationWeights, err := session.fusionWeights(
		[]string{secondRanking, "unsigned", firstRanking},
		map[string]yagomodel.Seed{
			firstRanking:  first,
			secondRanking: second,
			"unsigned":    {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(influenceWeights[firstRanking]-0.9) > 1e-12 ||
		math.Abs(influenceWeights[secondRanking]-0.6) > 1e-12 ||
		influenceWeights["unsigned"] != 1 {
		t.Fatalf("capped weights = %#v", influenceWeights)
	}
	if reputationWeights[firstRanking] != 1.5 ||
		reputationWeights[secondRanking] != 1 || reputationWeights["unsigned"] != 1 {
		t.Fatalf("reputation weights = %#v", reputationWeights)
	}
	identity := peerreputation.SignedPeerIdentity(first.Hash.String())
	session.networkGroup = nil
	if group := session.group(
		first,
		identity,
	); group != peerreputation.NetworkGroupKey(
		"peer:"+first.Hash.String(),
	) {
		t.Fatalf("default group = %q", group)
	}
	session.networkGroup = func(yagomodel.Seed) peerreputation.NetworkGroupKey { return "" }
	if group := session.group(
		first,
		identity,
	); group != peerreputation.NetworkGroupKey(
		"peer:"+first.Hash.String(),
	) {
		t.Fatalf("empty group fallback = %q", group)
	}
}

func TestResponseAppliesReputationAndFallsBackOnInvalidCap(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_800_000_000, 0).UTC()
	trusted := yagomodel.Seed{Hash: hashFor("APeer")}
	distrusted := yagomodel.Seed{Hash: hashFor("BPeer")}
	snapshot := reputationSnapshot(t, at, map[yagomodel.Hash]float64{
		trusted.Hash:    1.5,
		distrusted.Hash: 0.5,
	})
	remote := searcher{weights: DefaultRankingWeights}
	results := []peerSearchResult{
		peerResult(t, distrusted, "bad", "https://bad.example/"),
		peerResult(t, trusted, "good", "https://good.example/"),
	}
	session := &reputationSession{
		snapshot:              snapshot,
		snapshotAvailable:     true,
		maximumGroupInfluence: 1.5,
	}
	response := remote.response(t.Context(), searchcore.Request{
		Limit: 10, Verify: searchcore.VerifyFalse,
	}, results, session)
	if len(response.Results) != 2 || response.Results[0].URL != "https://good.example/" ||
		response.Results[0].Score != 1.5/61 || response.Results[1].Score != 0.5/61 {
		t.Fatalf("reputation response = %#v", response)
	}
	reputation, known := response.Results[0].Evidence.Value(searchcore.SignalPeerReputation)
	if !known || reputation != 1.5 {
		t.Fatalf("response reputation = %v, %v", reputation, known)
	}
	invalidCap := *session
	invalidCap.maximumGroupInfluence = math.Inf(1)
	fallback := remote.response(t.Context(), searchcore.Request{
		Limit: 10, Verify: searchcore.VerifyFalse,
	}, results, &invalidCap)
	if len(fallback.PartialFailures) != 1 ||
		fallback.PartialFailures[0].Source != searchcore.PartialFailureSourcePeerReputation ||
		fallback.Results[0].Score != 1.0/61.0 || fallback.Results[1].Score != 1.0/61.0 {
		t.Fatalf("invalid cap fallback = %#v", fallback)
	}
	for _, result := range fallback.Results {
		reputation, known := result.Evidence.Value(searchcore.SignalPeerReputation)
		if !known || reputation != 1 {
			t.Fatalf("fallback reputation = %v, %v", reputation, known)
		}
	}
}

func TestReputationObservationsAreStableAndSurviveCancellation(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_800_000_000, 0).UTC()
	sink := &capturedReputationObservations{}
	remote := NewSearcher(Config{
		ReputationObservations: sink,
		ReputationClock:        func() time.Time { return at },
		ReputationNetworkGroup: func(yagomodel.Seed) peerreputation.NetworkGroupKey {
			return "network"
		},
	}).(searcher)
	session, err := remote.beginReputation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	bodyOnly := yagomodel.Seed{Hash: hashFor("ABodyOnly")}
	timedOut := yagomodel.Seed{Hash: hashFor("BTimeout")}
	failed := yagomodel.Seed{Hash: hashFor("CFailure")}
	succeeded := yagomodel.Seed{Hash: hashFor("DSuccess")}
	unsigned := yagomodel.Seed{}
	response := remote.response(t.Context(), searchcore.Request{
		Terms: []string{"query"}, Limit: 10, Verify: searchcore.VerifyIfExist,
	}, []peerSearchResult{
		peerResult(t, succeeded, "query result", "https://success.example/"),
		{peer: failed, err: errors.New("connection failed")},
		{peer: unsigned, err: errors.New("unsigned failed")},
		peerResult(t, bodyOnly, "unrelated", "https://body-only.example/"),
		{peer: timedOut, err: context.DeadlineExceeded},
	}, session)
	if len(response.Results) != 2 || response.Results[0].URL != "https://body-only.example/" ||
		response.Results[1].URL != "https://success.example/" {
		t.Fatalf("observed response = %#v", response)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	session.flush(canceled)
	session.flush(canceled)
	if len(sink.batches) != 1 || sink.contextErrors[0] != nil {
		t.Fatalf("observation sink = %#v, %#v", sink.batches, sink.contextErrors)
	}
	observations := sink.batches[0]
	wantPeers := []peerreputation.SignedPeerIdentity{
		peerreputation.SignedPeerIdentity(bodyOnly.Hash.String()),
		peerreputation.SignedPeerIdentity(timedOut.Hash.String()),
		peerreputation.SignedPeerIdentity(failed.Hash.String()),
		peerreputation.SignedPeerIdentity(succeeded.Hash.String()),
	}
	gotPeers := make([]peerreputation.SignedPeerIdentity, len(observations))
	for index, observation := range observations {
		gotPeers[index] = observation.Peer
		if observation.NetworkGroup != "network" || !observation.ObservedAt.Equal(at) {
			t.Fatalf("observation %d = %+v", index, observation)
		}
	}
	if !slices.Equal(gotPeers, wantPeers) ||
		observations[0].Outcome != peerreputation.OutcomeSuccess ||
		observations[1].Outcome != peerreputation.OutcomeTimeout ||
		observations[2].Outcome != peerreputation.OutcomeFailure ||
		observations[3].Outcome != peerreputation.OutcomeSuccess {
		t.Fatalf("observations = %+v", observations)
	}
}

func TestSearcherReputationSnapshotLifecycle(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_800_000_000, 0).UTC()
	snapshot := reputationSnapshot(t, at, nil)
	source := &fixedReputationSnapshotSource{snapshot: snapshot}
	searcher := NewSearcher(Config{
		ReputationSnapshots:          source,
		MaximumNetworkGroupInfluence: 2,
		ReputationClock:              func() time.Time { return at },
	})
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Terms: []string{"query"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 || !source.at.Equal(at) || len(response.PartialFailures) != 1 {
		t.Fatalf("snapshot lifecycle = %+v, %#v", source, response)
	}
	failing := &fixedReputationSnapshotSource{err: errors.New("snapshot failed")}
	response, err = NewSearcher(Config{
		ReputationSnapshots: failing,
		ReputationClock:     func() time.Time { return at },
	}).Search(t.Context(), searchcore.Request{Terms: []string{"query"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.PartialFailures) != 2 ||
		response.PartialFailures[1].Source != searchcore.PartialFailureSourcePeerReputation ||
		!strings.Contains(response.PartialFailures[1].Reason, "snapshot failed") {
		t.Fatalf("snapshot failure = %#v", response.PartialFailures)
	}
	if maximumGroupInfluenceOrDefault(2) != 2 {
		t.Fatal("explicit group cap was replaced")
	}
	explicitClock := func() time.Time { return at }
	if !reputationClockOrDefault(explicitClock)().Equal(at) {
		t.Fatal("explicit reputation clock was replaced")
	}
}

func TestMalformedDeclaredPeerRowsRecordInvalidOutcome(t *testing.T) {
	sink := &capturedReputationObservations{}
	remote := NewSearcher(Config{
		ReputationObservations: sink,
		ReputationNetworkGroup: func(yagomodel.Seed) peerreputation.NetworkGroupKey {
			return "network"
		},
	}).(searcher)
	_, readErr := readRemoteSearchResponse(strings.NewReader("count=1\nresource0=bad\n"))
	if !errors.Is(readErr, errRemoteSearchInvalidResult) {
		t.Fatalf("malformed response error = %v", readErr)
	}
	session, err := remote.beginReputation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	peer := yagomodel.Seed{Hash: hashFor("malformed")}
	response := remote.response(
		t.Context(),
		searchcore.Request{Limit: 10},
		[]peerSearchResult{{peer: peer, err: readErr}},
		session,
	)
	session.flush(t.Context())
	if len(response.Results) != 0 || len(response.PartialFailures) != 1 ||
		len(sink.batches) != 1 || len(sink.batches[0]) != 1 ||
		sink.batches[0][0].Outcome != peerreputation.OutcomeInvalidResult {
		t.Fatalf("malformed peer observation = %#v, %#v", response, sink.batches)
	}
}

type fixedReputationSnapshotSource struct {
	snapshot peerreputation.Snapshot
	err      error
	calls    int
	at       time.Time
}

func (source *fixedReputationSnapshotSource) Snapshot(
	_ context.Context,
	at time.Time,
) (peerreputation.Snapshot, error) {
	source.calls++
	source.at = at

	return source.snapshot, source.err
}

type capturedReputationObservations struct {
	batches       [][]peerreputation.Observation
	contextErrors []error
}

func (sink *capturedReputationObservations) Observe(
	ctx context.Context,
	observations []peerreputation.Observation,
) {
	sink.batches = append(sink.batches, slices.Clone(observations))
	sink.contextErrors = append(sink.contextErrors, ctx.Err())
}

func reputationSnapshot(
	t *testing.T,
	at time.Time,
	weights map[yagomodel.Hash]float64,
) peerreputation.Snapshot {
	t.Helper()
	peers := make([]peerreputation.PeerReputation, 0, len(weights))
	for identity, weight := range weights {
		peers = append(peers, peerreputation.PeerReputation{
			Peer:                 peerreputation.SignedPeerIdentity(identity.String()),
			NetworkGroup:         "stored",
			Known:                true,
			Reliability:          weight / 2,
			FusionWeight:         weight,
			Confidence:           0.75,
			SuccessEvidence:      3,
			FailureEvidence:      1,
			LastObservedUnixNano: at.UnixNano(),
		})
	}
	payload := struct {
		Version             int                             `json:"version"`
		GeneratedAtUnixNano int64                           `json:"generated_at_unix_nano"`
		NeutralReliability  float64                         `json:"neutral_reliability"`
		Peers               []peerreputation.PeerReputation `json:"peers"`
	}{
		Version:             1,
		GeneratedAtUnixNano: at.UnixNano(),
		NeutralReliability:  0.5,
		Peers:               peers,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot peerreputation.Snapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		t.Fatal(err)
	}

	return snapshot
}

func peerResult(
	t *testing.T,
	peer yagomodel.Seed,
	title string,
	rawURL string,
) peerSearchResult {
	t.Helper()
	resultHash := peer.Hash
	if resultHash == "" {
		resultHash = hashFor("DocResult")
	}

	return peerSearchResult{
		peer: peer,
		response: yagoproto.SearchResponse{Resources: []yagomodel.URIMetadataRow{
			metadataRow(t, resultHash, rawURL, title),
		}},
	}
}

func countReputationOutcome(
	observations []peerreputation.Observation,
	outcome peerreputation.Outcome,
) int {
	total := 0
	for _, observation := range observations {
		if observation.Outcome == outcome {
			total++
		}
	}

	return total
}
