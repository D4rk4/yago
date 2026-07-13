package searchremote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	DefaultMaxPeers   = 8
	DefaultRedundancy = 3
	// DefaultConcurrency caps the peer fan-out workers. YaCy queries every
	// selected target at once (RemoteSearch.java starts one thread per peer),
	// so the cap exists only to bound a pathological roster: with a
	// four-worker pool, thirteen targets took four sequential waves of up to
	// three seconds each and the overall budget expired before the later
	// waves even started — every peer reported as timed out (SEARCH-37).
	DefaultConcurrency        = 32
	DefaultPerPeerTimeout     = 1200 * time.Millisecond
	DefaultOverallTimeout     = 1300 * time.Millisecond
	DefaultMinimumPeerAgeDays = 3
	DefaultMinimumPeerRWIs    = 1
	maxPartitionExponent      = 8
	secondaryURLCap           = 128
	remoteSearchBodyCap       = 512 << 10
)

var (
	errRemoteSearchFailed        = errors.New("remote search failed")
	errRemoteSearchTransport     = errors.New("peer transport failed")
	errRemoteSearchInvalidResult = errors.New("peer returned invalid search response")
)

// PeerSource supplies the candidate peers a remote search may target. Following
// YaCy, these are known senior peers selected from the seed database by DHT
// position — NOT only peers confirmed reachable by a prior hello handshake — so
// a node whose inbound reachability never completes (e.g. behind NAT) can still
// search the network. Unreachable candidates surface as per-peer partial
// failures rather than blocking the search outright.
type PeerSource interface {
	SearchTargetPeers(ctx context.Context) []yagomodel.Seed
}

type Config struct {
	Client                       *http.Client
	NetworkName                  string
	Peers                        PeerSource
	MaxPeers                     int
	Redundancy                   int
	MinimumPeerAgeDays           int
	MinimumPeerRWIs              int
	PartitionExponent            int
	Concurrency                  int
	PerPeerTimeout               time.Duration
	OverallTimeout               time.Duration
	RandomTargetIndex            func(int) (int, error)
	Weights                      func() RankingWeights
	PreferHTTPS                  bool
	ReputationSnapshots          ReputationSnapshotSource
	ReputationObservations       ReputationObservationSink
	ReputationNetworkGroup       ReputationNetworkGroup
	MaximumNetworkGroupInfluence float64
	ReputationClock              func() time.Time
	SelfSeed                     func(context.Context) yagomodel.Seed
	// ExpandWord returns the inflected surface forms to also search for a
	// single-word query; nil disables swarm morphology.
	ExpandWord func(string) []string
}

type searcher struct {
	client                       *http.Client
	networkName                  string
	peers                        PeerSource
	maxPeers                     int
	redundancy                   int
	minimumPeerAgeDays           int
	minimumPeerRWIs              int
	partitionExponent            int
	concurrency                  int
	perPeerTimeout               time.Duration
	overallTimeout               time.Duration
	randomTargetIndex            func(int) (int, error)
	weights                      func() RankingWeights
	preferHTTPS                  bool
	expandWord                   func(string) []string
	reputationSnapshots          ReputationSnapshotSource
	reputationObservations       ReputationObservationSink
	reputationNetworkGroup       ReputationNetworkGroup
	maximumNetworkGroupInfluence float64
	reputationClock              func() time.Time
	selfSeed                     func(context.Context) yagomodel.Seed
	fetchAdmission               chan struct{}
}

type peerSearchResult struct {
	term               yagomodel.Hash
	peer               yagomodel.Seed
	response           yagoproto.SearchResponse
	err                error
	responseBytes      int
	resourcesTruncated bool
	responseIncomplete bool
}

type peerSearchJob struct {
	term                yagomodel.Hash
	peer                yagomodel.Seed
	request             yagoproto.SearchRequest
	responseBodyLimit   int
	responseBodyLimited bool
}

type termPeerTargets struct {
	term  yagomodel.Hash
	peers []yagomodel.Seed
}

var newRemoteSearchRequest = http.NewRequestWithContext

func NewSearcher(config Config) searchcore.Searcher {
	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}
	return searcher{
		client:                 client,
		networkName:            config.NetworkName,
		peers:                  config.Peers,
		maxPeers:               positiveOrDefault(config.MaxPeers, DefaultMaxPeers),
		redundancy:             positiveOrDefault(config.Redundancy, DefaultRedundancy),
		minimumPeerAgeDays:     defaultMinimumPeerAgeDays(config.MinimumPeerAgeDays),
		minimumPeerRWIs:        defaultMinimumPeerRWIs(config.MinimumPeerRWIs),
		partitionExponent:      normalizedPartitionExponent(config.PartitionExponent),
		concurrency:            positiveOrDefault(config.Concurrency, DefaultConcurrency),
		perPeerTimeout:         durationOrDefault(config.PerPeerTimeout, DefaultPerPeerTimeout),
		overallTimeout:         durationOrDefault(config.OverallTimeout, DefaultOverallTimeout),
		randomTargetIndex:      randomTargetIndexOrDefault(config.RandomTargetIndex),
		weights:                weightsOrDefault(config.Weights),
		preferHTTPS:            config.PreferHTTPS,
		expandWord:             config.ExpandWord,
		reputationSnapshots:    config.ReputationSnapshots,
		reputationObservations: config.ReputationObservations,
		reputationNetworkGroup: config.ReputationNetworkGroup,
		maximumNetworkGroupInfluence: maximumGroupInfluenceOrDefault(
			config.MaximumNetworkGroupInfluence,
		),
		reputationClock: reputationClockOrDefault(config.ReputationClock),
		selfSeed:        config.SelfSeed,
	}
}

func (s searcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	ctx, cancel := s.overallContext(ctx)
	defer cancel()
	s = s.withSelfSeedSnapshot(ctx)
	reputation, reputationErr := s.beginReputation(ctx)
	budget := newRemoteQueryBudget()

	var response searchcore.Response
	if variants := s.swarmMorphologyVariants(req); len(variants) > 1 {
		response = s.searchVariants(ctx, req, variants, reputation, budget)
	} else {
		response = s.searchExact(ctx, req, reputation, budget)
	}
	reputation.flush(ctx)
	if reputationErr != nil {
		response.PartialFailures = append(response.PartialFailures, searchcore.PartialFailure{
			Source: searchcore.PartialFailureSourcePeerReputation,
			Reason: reputationErr.Error(),
		})
	}

	return response, nil
}

func (s searcher) searchExact(
	ctx context.Context,
	req searchcore.Request,
	reputation *reputationSession,
	budget *remoteQueryBudget,
) searchcore.Response {
	hashes := termHashes(req.Terms)
	peers, noPeersReason := s.remotePeers(ctx, hashes)
	if len(peers) == 0 {
		return searchcore.Response{
			Request: req,
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceRemoteYaCy,
				Reason: noPeersReason,
			}},
		}
	}

	// Primary search: send every query word hash to the selected DHT targets in a
	// single request so each peer performs the AND-intersection over its own index
	// and returns documents matching all words. This mirrors YaCy's primary remote
	// search (search.java searchConjunction) and is the main result source for
	// both single- and multi-word queries.
	results := s.queryPeers(ctx, peers, req, budget)

	// For multi-word queries also run the index-abstract secondary search, which
	// recovers documents whose matching words are held by different peers (no
	// single peer holds all words). It is a best-effort enhancement layered on top
	// of the primary results, not a replacement for them.
	if len(hashes) > 1 {
		secondaryResults, secondaryFailures := s.secondaryAbstractSearch(
			ctx,
			req,
			hashes,
			reputation,
			budget,
		)
		resp := s.responseWithinBudget(
			ctx,
			req,
			append(results, secondaryResults...),
			reputation,
			budget,
		)
		resp.PartialFailures = append(secondaryFailures, resp.PartialFailures...)

		return resp
	}

	return s.responseWithinBudget(ctx, req, results, reputation, budget)
}

// swarmMorphologyVariants returns the surface-form variants to search for a
// single-word query, so the swarm matches peer documents indexed under an
// inflected form of the query word. It is disabled when no expander is wired,
// and multi-word queries keep the exact conjunctive search — expanding several
// words would multiply the peer fan-out.
func (s searcher) swarmMorphologyVariants(req searchcore.Request) []string {
	if s.expandWord == nil || len(req.Terms) != 1 {
		return nil
	}

	return s.expandWord(req.Terms[0])
}

// searchVariants runs the exact conjunctive search once per surface variant of a
// single-word query and fuses the passes by reciprocal rank, so a document
// indexed under any inflection contributes while the base form keeps top rank.
// Each variant is wire-compatible: its hash is an ordinary exact-word hash.
func (s searcher) searchVariants(
	ctx context.Context,
	req searchcore.Request,
	variants []string,
	reputation *reputationSession,
	budget *remoteQueryBudget,
) searchcore.Response {
	lists := make([][]searchcore.Result, 0, len(variants))
	failures := make([]searchcore.PartialFailure, 0, len(variants))
	for _, variant := range variants {
		if ctx.Err() != nil {
			break
		}
		variantReq := req
		variantReq.Terms = []string{variant}
		variantReq.Query = variant
		resp := s.searchExact(ctx, variantReq, reputation, budget)
		lists = append(lists, resp.Results)
		failures = append(failures, resp.PartialFailures...)
	}

	fused := searchcore.FuseByReciprocalRank(lists...)
	// Fusion deduplicates across the variant passes, so the fused length is the
	// honest total; summing per-variant totals would double-count documents
	// indexed under several inflections.
	total := len(fused)
	if len(fused) > req.Limit && req.Limit > 0 {
		fused = fused[:req.Limit]
	}

	return searchcore.Response{
		Request:         req,
		TotalResults:    total,
		Results:         fused,
		PartialFailures: failures,
	}
}

func (s searcher) overallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.overallTimeout)
}

func (s searcher) remotePeers(
	ctx context.Context,
	hashes []yagomodel.Hash,
) ([]yagomodel.Seed, string) {
	if s.peers == nil {
		return nil, "no peer source configured"
	}
	peers := s.peers.SearchTargetPeers(ctx)
	if len(peers) == 0 {
		return nil, "no known peers"
	}

	if len(hashes) == 0 {
		return nil, "no query terms"
	}

	selected, err := selectDHTSearchPeers(hashes, peers, dhtSearchPeerConfig{
		maxPeers:           s.maxPeers,
		redundancy:         s.redundancy,
		minimumPeerAgeDays: s.minimumPeerAgeDays,
		minimumPeerRWIs:    s.minimumPeerRWIs,
		partitionExponent:  s.partitionExponent,
		randomTargetIndex:  s.randomTargetIndex,
	})
	if err != nil {
		return nil, err.Error()
	}
	if len(selected) == 0 {
		return nil, "no dht search targets"
	}

	return selected, ""
}

func (s searcher) queryPeers(
	ctx context.Context,
	peers []yagomodel.Seed,
	req searchcore.Request,
	budget *remoteQueryBudget,
) []peerSearchResult {
	searchReq := remoteSearchRequest(req, s.networkName, s.perPeerTimeout)
	jobs := make([]peerSearchJob, 0, len(peers))
	for _, peer := range peers {
		jobs = append(jobs, peerSearchJob{peer: peer, request: searchReq})
	}

	return s.queryPeerJobsWithinBudget(ctx, jobs, budget)
}

func (s searcher) queryPeerJobs(
	ctx context.Context,
	requests []peerSearchJob,
) []peerSearchResult {
	return s.queryPeerJobsWithinBudget(ctx, requests, newRemoteQueryBudget())
}

func (s searcher) queryPeerJob(
	ctx context.Context,
	job peerSearchJob,
) peerSearchResult {
	if job.responseBodyLimited && job.responseBodyLimit == 0 {
		return peerSearchResult{
			term: job.term,
			peer: job.peer,
			err:  errRemoteSearchBudgetExhausted,
		}
	}
	peerCtx, cancel := context.WithTimeout(ctx, s.perPeerTimeout)
	defer cancel()

	responseBodyLimit := remoteSearchBodyCap
	if job.responseBodyLimited {
		responseBodyLimit = job.responseBodyLimit
	}
	resp, responseBytes, err := s.sendRemoteSearchWithinLimit(
		peerCtx,
		job.peer,
		job.request,
		responseBodyLimit,
	)
	return peerSearchResult{
		term:          job.term,
		peer:          job.peer,
		response:      resp,
		err:           err,
		responseBytes: responseBytes,
	}
}

// secondaryAbstractSearch runs YaCy's two-phase index-abstract search as a
// best-effort enhancement for multi-word queries: it asks each word's peers for
// the URL-hash abstract of that word, intersects the abstracts to find URLs that
// contain every word across the network, then fetches those URLs' metadata. It
// returns the secondary peer results to merge with the primary search plus any
// partial failures gathered along the way. A word with no reachable target, an
// empty abstract, or an empty intersection simply yields no secondary results;
// the primary search still stands.
func (s searcher) secondaryAbstractSearch(
	ctx context.Context,
	req searchcore.Request,
	terms []yagomodel.Hash,
	reputation *reputationSession,
	budget *remoteQueryBudget,
) ([]peerSearchResult, []searchcore.PartialFailure) {
	targets, failures := s.termTargets(ctx, terms)

	abstracts, abstractFailures := s.termAbstractsWithinBudget(
		ctx,
		req,
		targets,
		reputation,
		budget,
	)
	failures = append(failures, abstractFailures...)
	urls := intersectTermAbstracts(terms, abstracts)
	if len(urls) == 0 {
		return nil, failures
	}

	results := s.queryPeerJobsWithinBudget(ctx, secondarySearchJobs(
		req,
		targets,
		limitSecondaryURLs(req, urls),
		s.networkName,
		s.perPeerTimeout,
	), budget)

	return results, failures
}

func (s searcher) termTargets(
	ctx context.Context,
	terms []yagomodel.Hash,
) ([]termPeerTargets, []searchcore.PartialFailure) {
	targets := make([]termPeerTargets, 0, len(terms))
	var failures []searchcore.PartialFailure
	for _, term := range terms {
		peers, reason := s.remotePeers(ctx, []yagomodel.Hash{term})
		if len(peers) == 0 {
			failures = append(failures, searchcore.PartialFailure{
				Source: searchcore.PartialFailureSourceRemoteYaCy,
				Reason: fmt.Sprintf("no dht search targets for %s: %s", term, reason),
			})
			continue
		}
		targets = append(targets, termPeerTargets{term: term, peers: peers})
	}

	return targets, failures
}

func (s searcher) termAbstracts(
	ctx context.Context,
	req searchcore.Request,
	targets []termPeerTargets,
	reputation *reputationSession,
) (map[yagomodel.Hash]map[yagomodel.Hash]struct{}, []searchcore.PartialFailure) {
	return s.termAbstractsWithinBudget(
		ctx,
		req,
		targets,
		reputation,
		newRemoteQueryBudget(),
	)
}

func abstractSearchJobs(
	req searchcore.Request,
	targets []termPeerTargets,
	networkName string,
	perPeerTimeout time.Duration,
) []peerSearchJob {
	var jobs []peerSearchJob
	for _, target := range targets {
		searchReq := abstractRemoteSearchRequest(req, target.term, networkName, perPeerTimeout)
		for _, peer := range target.peers {
			jobs = append(jobs, peerSearchJob{
				term:    target.term,
				peer:    peer,
				request: searchReq,
			})
		}
	}

	return jobs
}

func secondarySearchJobs(
	req searchcore.Request,
	targets []termPeerTargets,
	urls []yagomodel.Hash,
	networkName string,
	perPeerTimeout time.Duration,
) []peerSearchJob {
	var jobs []peerSearchJob
	for _, target := range targets {
		searchReq := secondaryRemoteSearchRequest(
			req,
			target.term,
			urls,
			networkName,
			perPeerTimeout,
		)
		for _, peer := range target.peers {
			jobs = append(jobs, peerSearchJob{
				term:    target.term,
				peer:    peer,
				request: searchReq,
			})
		}
	}

	return jobs
}

func intersectTermAbstracts(
	terms []yagomodel.Hash,
	abstracts map[yagomodel.Hash]map[yagomodel.Hash]struct{},
) []yagomodel.Hash {
	first := abstracts[terms[0]]
	if len(first) == 0 {
		return nil
	}
	intersection := make(map[yagomodel.Hash]struct{}, len(first))
	for hash := range first {
		intersection[hash] = struct{}{}
	}
	for _, term := range terms[1:] {
		next := abstracts[term]
		if len(next) == 0 {
			return nil
		}
		for hash := range intersection {
			if _, ok := next[hash]; !ok {
				delete(intersection, hash)
			}
		}
	}

	return sortedHashes(intersection)
}

func sortedHashes(hashes map[yagomodel.Hash]struct{}) []yagomodel.Hash {
	out := make([]yagomodel.Hash, 0, len(hashes))
	for hash := range hashes {
		out = append(out, hash)
	}
	slices.SortFunc(out, func(a, b yagomodel.Hash) int {
		return strings.Compare(a.String(), b.String())
	})

	return out
}

func limitSecondaryURLs(
	req searchcore.Request,
	urls []yagomodel.Hash,
) []yagomodel.Hash {
	limit := req.Offset + req.Limit
	if limit <= 0 {
		limit = searchcore.DefaultPublicLimit
	}
	if limit > secondaryURLCap {
		limit = secondaryURLCap
	}
	if len(urls) <= limit {
		return urls
	}

	return urls[:limit]
}

func (s searcher) remoteSearch(
	ctx context.Context,
	peer yagomodel.Seed,
	req searchcore.Request,
) (yagoproto.SearchResponse, error) {
	return s.sendRemoteSearch(
		ctx,
		peer,
		remoteSearchRequest(req, s.networkName, s.perPeerTimeout),
	)
}

func (s searcher) sendRemoteSearch(
	ctx context.Context,
	peer yagomodel.Seed,
	searchReq yagoproto.SearchRequest,
) (yagoproto.SearchResponse, error) {
	response, _, err := s.sendRemoteSearchWithinLimit(
		ctx,
		peer,
		searchReq,
		remoteSearchBodyCap,
	)

	return response, err
}

func baseRemoteSearchRequest(
	req searchcore.Request,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	return yagoproto.SearchRequest{
		NetworkName: networkName,
		Exclude:     termHashes(req.ExcludedTerms),
		Count:       req.Limit,
		Time:        int(perPeerTimeout / time.Millisecond),
		ContentDom:  yagoproto.SearchContentDomain(req.ContentDomain),
		Language:    req.Language,
		Prefer:      req.PreferMaskFilter,
		Filter:      req.URLMaskFilter,
		SiteHost:    req.SiteHost,
		FileType:    req.FileType,
	}
}

func abstractRemoteSearchRequest(
	req searchcore.Request,
	term yagomodel.Hash,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	searchReq := baseRemoteSearchRequest(req, networkName, perPeerTimeout)
	searchReq.Abstracts = yagoproto.SearchAbstracts(term.String())

	return searchReq
}

func secondaryRemoteSearchRequest(
	req searchcore.Request,
	term yagomodel.Hash,
	urls []yagomodel.Hash,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	searchReq := baseRemoteSearchRequest(req, networkName, perPeerTimeout)
	searchReq.Query = []yagomodel.Hash{term}
	searchReq.URLs = urls
	searchReq.Count = len(urls)

	return searchReq
}

func remoteSearchRequest(
	req searchcore.Request,
	networkName string,
	perPeerTimeout time.Duration,
) yagoproto.SearchRequest {
	searchReq := baseRemoteSearchRequest(req, networkName, perPeerTimeout)
	searchReq.Query = termHashes(req.Terms)

	return searchReq
}

func termHashes(terms []string) []yagomodel.Hash {
	hashes := make([]yagomodel.Hash, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		hashes = append(hashes, yagomodel.WordHash(term))
	}

	return hashes
}

func readRemoteSearchResponse(body io.Reader) (yagoproto.SearchResponse, error) {
	response, _, err := readRemoteSearchResponseWithinLimit(body, remoteSearchBodyCap)

	return response, err
}

func (s searcher) response(
	ctx context.Context,
	req searchcore.Request,
	results []peerSearchResult,
	reputation *reputationSession,
) searchcore.Response {
	return s.responseWithinBudget(ctx, req, results, reputation, newRemoteQueryBudget())
}

func (s searcher) responseWithinBudget(
	ctx context.Context,
	req searchcore.Request,
	results []peerSearchResult,
	reputation *reputationSession,
	budget *remoteQueryBudget,
) searchcore.Response {
	var resp searchcore.Response
	resp.Request = req
	scorer := newRemoteScorer(req.Terms, s.weights())
	peerOrder := make([]string, 0, len(results))
	peerResults := make(map[string][]searchcore.Result, len(results))
	peerSeeds := make(map[string]yagomodel.Seed, len(results))
	for _, result := range orderedPeerSearchResults(results) {
		if result.err != nil {
			recordPeerFailure(reputation, result.peer, result.err)
			resp.PartialFailures = append(
				resp.PartialFailures,
				peerFailure(result.peer, result.err),
			)
			continue
		}
		normalized, err := searchResultsWithinBudget(
			ctx,
			req,
			result.response.Resources,
			scorer,
			budget,
		)
		if err != nil {
			if errors.Is(err, errRemoteSearchDecodedBudgetExhausted) {
				resp.PartialFailures = append(resp.PartialFailures, searchcore.PartialFailure{
					Source: searchcore.PartialFailureSourceRemoteYaCy,
					Reason: err.Error(),
				})
				break
			}
			reputation.record(result.peer, observationOutcome(nil, true))
			resp.PartialFailures = append(resp.PartialFailures, peerFailure(result.peer, err))
			continue
		}
		invalid := result.responseIncomplete ||
			(!result.resourcesTruncated &&
				result.response.Count > len(result.response.Resources)) ||
			len(normalized) < len(cappedPeerRows(
				result.response.Resources,
				req.Offset+req.Limit,
			))
		reputation.record(result.peer, observationOutcome(nil, invalid))
		if len(normalized) == 0 {
			continue
		}
		identity := peerRankingIdentity(result.peer)
		if _, found := peerResults[identity]; !found {
			peerOrder = append(peerOrder, identity)
			peerSeeds[identity] = result.peer
		}
		peerResults[identity] = append(peerResults[identity], normalized...)
	}
	fused, reputationErr := fusedPeerResponseResults(
		reputation,
		peerOrder,
		peerResults,
		peerSeeds,
	)
	if reputationErr != nil {
		resp.PartialFailures = append(resp.PartialFailures, searchcore.PartialFailure{
			Source: searchcore.PartialFailureSourcePeerReputation,
			Reason: reputationErr.Error(),
		})
	}
	// The honest remote total is the verified, deduplicated rows actually in
	// hand, not the peers' claimed join counts: an unverifiable claim must not
	// inflate the result counter or fabricate result pages.
	resp.TotalResults = len(fused)
	resp.Results = offsetResults(fused, req.Offset, req.Limit)

	return resp
}

func dedupeSearchResults(results []searchcore.Result) []searchcore.Result {
	out := make([]searchcore.Result, 0, len(results))
	seen := map[string]struct{}{}
	for _, result := range results {
		identity := remoteResultIdentity(result)
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		out = append(out, result)
	}

	return out
}

func remoteResultIdentity(result searchcore.Result) string {
	if result.URLHash != "" {
		return "hash:" + result.URLHash
	}

	return "url:" + result.URL
}

func peerFailure(peer yagomodel.Seed, err error) searchcore.PartialFailure {
	source := peer.Hash.String()
	if source == "" {
		source = searchcore.PartialFailureSourceRemoteYaCy
	}

	return searchcore.PartialFailure{Source: source, Reason: err.Error()}
}

func searchResults(
	ctx context.Context,
	req searchcore.Request,
	rows []yagomodel.URIMetadataRow,
	scorer remoteScorer,
) ([]searchcore.Result, error) {
	return searchResultsWithinBudget(ctx, req, rows, scorer, newRemoteQueryBudget())
}

func searchResultsWithinBudget(
	ctx context.Context,
	req searchcore.Request,
	rows []yagomodel.URIMetadataRow,
	scorer remoteScorer,
	budget *remoteQueryBudget,
) ([]searchcore.Result, error) {
	rows = cappedPeerRows(rows, req.Offset+req.Limit)
	results := make([]searchcore.Result, 0, len(rows))
	for i, row := range rows {
		result, err := searchResultWithinBudget(ctx, req, row, budget)
		if err != nil {
			return nil, err
		}
		result.Score = scorer.score(result, i, len(rows))
		result.Evidence = searchcore.NewRankingEvidence(
			searchcore.RankingSignalValue{
				Signal: searchcore.SignalRetrievalScore,
				Value:  result.Score,
			},
			searchcore.RankingSignalValue{
				Signal: searchcore.SignalRemoteRank,
				Value:  float64(i + 1),
			},
			searchcore.RankingSignalValue{
				Signal: searchcore.SignalPeerSupport,
				Value:  1,
			},
		)
		results = append(results, result)
	}

	return rankRemoteResults(languageFiltered(req, results)), nil
}

func searchResult(
	ctx context.Context,
	row yagomodel.URIMetadataRow,
) (searchcore.Result, error) {
	return searchResultWithinBudget(ctx, searchcore.Request{}, row, newRemoteQueryBudget())
}

func searchResultWithinBudget(
	ctx context.Context,
	req searchcore.Request,
	row yagomodel.URIMetadataRow,
	budget *remoteQueryBudget,
) (searchcore.Result, error) {
	rawURL, err := decodedMetadataPropertyWithinBudget(
		ctx,
		row,
		yagomodel.URLMetaURL,
		remoteMetadataURLByteLimit,
		budget,
	)
	if err != nil {
		return searchcore.Result{}, err
	}
	title, err := decodedMetadataPropertyWithinBudget(
		ctx,
		row,
		yagomodel.URLMetaColDescription,
		remoteMetadataTitleByteLimit,
		budget,
	)
	if err != nil {
		return searchcore.Result{}, err
	}
	hash, err := row.URLHash()
	if err != nil {
		return searchcore.Result{}, fmt.Errorf("url metadata hash: %w", err)
	}
	parsed, _ := url.Parse(rawURL)
	host, pathValue, file := parsedURLParts(parsed)
	if title == "" {
		title = rawURL
	}
	language, err := boundedRowLanguage(row, budget)
	if err != nil {
		return searchcore.Result{}, err
	}

	return searchcore.Result{
		Title:         title,
		URL:           rawURL,
		DisplayURL:    displayURL(host, pathValue),
		Snippet:       title,
		Source:        searchcore.SourceRemote,
		Host:          host,
		Path:          pathValue,
		File:          file,
		URLHash:       hash.String(),
		Size:          metadataSize(row),
		ContentDomain: req.ContentDomain,
		Language:      language,
	}, nil
}

func parsedURLParts(parsed *url.URL) (string, string, string) {
	if parsed == nil {
		return "", "", ""
	}
	pathValue := parsed.EscapedPath()
	file := path.Base(parsed.Path)
	if file == "." || file == "/" || strings.HasSuffix(parsed.Path, "/") {
		file = ""
	}

	return parsed.Hostname(), pathValue, file
}

func displayURL(host, pathValue string) string {
	if host == "" {
		return pathValue
	}

	return host + pathValue
}

func metadataSize(row yagomodel.URIMetadataRow) int {
	size, _ := strconv.Atoi(row.Properties["size"])

	return size
}

func offsetResults(results []searchcore.Result, offset int, limit int) []searchcore.Result {
	if offset >= len(results) {
		return nil
	}
	end := offset + limit
	if end > len(results) {
		end = len(results)
	}

	return results[offset:end]
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}

	return fallback
}

func durationOrDefault(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}

	return fallback
}

func defaultMinimumPeerAgeDays(value int) int {
	if value != 0 {
		return value
	}

	return DefaultMinimumPeerAgeDays
}

func defaultMinimumPeerRWIs(value int) int {
	if value != 0 {
		return value
	}

	return DefaultMinimumPeerRWIs
}

func normalizedPartitionExponent(value int) int {
	if value < 0 {
		return 0
	}
	if value > maxPartitionExponent {
		return maxPartitionExponent
	}

	return value
}
