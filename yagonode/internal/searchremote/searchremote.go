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
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	DefaultMaxPeers           = 8
	DefaultRedundancy         = 3
	DefaultConcurrency        = 4
	DefaultPerPeerTimeout     = time.Second
	DefaultOverallTimeout     = 2 * time.Second
	DefaultMinimumPeerAgeDays = 3
	DefaultMinimumPeerRWIs    = 1
	maxPartitionExponent      = 8
	secondaryURLCap           = 128
	remoteSearchBodyCap       = 512 << 10
)

var errRemoteSearchFailed = errors.New("remote search failed")

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
	Client             *http.Client
	NetworkName        string
	Peers              PeerSource
	MaxPeers           int
	Redundancy         int
	MinimumPeerAgeDays int
	MinimumPeerRWIs    int
	PartitionExponent  int
	Concurrency        int
	PerPeerTimeout     time.Duration
	OverallTimeout     time.Duration
	RandomTargetIndex  func(int) (int, error)
}

type searcher struct {
	client             *http.Client
	networkName        string
	peers              PeerSource
	maxPeers           int
	redundancy         int
	minimumPeerAgeDays int
	minimumPeerRWIs    int
	partitionExponent  int
	concurrency        int
	perPeerTimeout     time.Duration
	overallTimeout     time.Duration
	randomTargetIndex  func(int) (int, error)
}

type peerSearchResult struct {
	term     yagomodel.Hash
	peer     yagomodel.Seed
	response yagoproto.SearchResponse
	err      error
}

type peerSearchJob struct {
	term    yagomodel.Hash
	peer    yagomodel.Seed
	request yagoproto.SearchRequest
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
		client:             client,
		networkName:        config.NetworkName,
		peers:              config.Peers,
		maxPeers:           positiveOrDefault(config.MaxPeers, DefaultMaxPeers),
		redundancy:         positiveOrDefault(config.Redundancy, DefaultRedundancy),
		minimumPeerAgeDays: defaultMinimumPeerAgeDays(config.MinimumPeerAgeDays),
		minimumPeerRWIs:    defaultMinimumPeerRWIs(config.MinimumPeerRWIs),
		partitionExponent:  normalizedPartitionExponent(config.PartitionExponent),
		concurrency:        positiveOrDefault(config.Concurrency, DefaultConcurrency),
		perPeerTimeout:     durationOrDefault(config.PerPeerTimeout, DefaultPerPeerTimeout),
		overallTimeout:     durationOrDefault(config.OverallTimeout, DefaultOverallTimeout),
		randomTargetIndex:  randomTargetIndexOrDefault(config.RandomTargetIndex),
	}
}

func (s searcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	ctx, cancel := s.overallContext(ctx)
	defer cancel()

	hashes := termHashes(req.Terms)
	peers, noPeersReason := s.remotePeers(ctx, hashes)
	if len(peers) == 0 {
		return searchcore.Response{
			Request: req,
			PartialFailures: []searchcore.PartialFailure{{
				Source: "remote-yacy",
				Reason: noPeersReason,
			}},
		}, nil
	}

	// Primary search: send every query word hash to the selected DHT targets in a
	// single request so each peer performs the AND-intersection over its own index
	// and returns documents matching all words. This mirrors YaCy's primary remote
	// search (search.java searchConjunction) and is the main result source for
	// both single- and multi-word queries.
	results := s.queryPeers(ctx, peers, req)

	// For multi-word queries also run the index-abstract secondary search, which
	// recovers documents whose matching words are held by different peers (no
	// single peer holds all words). It is a best-effort enhancement layered on top
	// of the primary results, not a replacement for them.
	if len(hashes) > 1 {
		secondaryResults, secondaryFailures := s.secondaryAbstractSearch(ctx, req, hashes)
		resp := s.response(ctx, req, append(results, secondaryResults...))
		resp.PartialFailures = append(secondaryFailures, resp.PartialFailures...)

		return resp, nil
	}

	return s.response(ctx, req, results), nil
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
) []peerSearchResult {
	searchReq := remoteSearchRequest(req, s.networkName, s.perPeerTimeout)
	jobs := make([]peerSearchJob, 0, len(peers))
	for _, peer := range peers {
		jobs = append(jobs, peerSearchJob{peer: peer, request: searchReq})
	}

	return s.queryPeerJobs(ctx, jobs)
}

func (s searcher) queryPeerJobs(
	ctx context.Context,
	requests []peerSearchJob,
) []peerSearchResult {
	if len(requests) == 0 {
		return nil
	}

	workerCount := min(s.concurrency, len(requests))
	jobs := make(chan peerSearchJob)
	results := make(chan peerSearchResult, len(requests))
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				results <- s.queryPeerJob(ctx, job)
			}
		}()
	}
	for _, job := range requests {
		jobs <- job
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]peerSearchResult, 0, len(requests))
	for result := range results {
		out = append(out, result)
	}

	return out
}

func (s searcher) queryPeerJob(
	ctx context.Context,
	job peerSearchJob,
) peerSearchResult {
	peerCtx, cancel := context.WithTimeout(ctx, s.perPeerTimeout)
	defer cancel()

	resp, err := s.sendRemoteSearch(peerCtx, job.peer, job.request)
	return peerSearchResult{
		term:     job.term,
		peer:     job.peer,
		response: resp,
		err:      err,
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
) ([]peerSearchResult, []searchcore.PartialFailure) {
	targets, failures := s.termTargets(ctx, terms)

	abstracts, abstractFailures := s.termAbstracts(ctx, req, targets)
	failures = append(failures, abstractFailures...)
	urls := intersectTermAbstracts(terms, abstracts)
	if len(urls) == 0 {
		return nil, failures
	}

	results := s.queryPeerJobs(ctx, secondarySearchJobs(
		req,
		targets,
		limitSecondaryURLs(req, urls),
		s.networkName,
		s.perPeerTimeout,
	))

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
				Source: "remote-yacy",
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
) (map[yagomodel.Hash]map[yagomodel.Hash]struct{}, []searchcore.PartialFailure) {
	results := s.queryPeerJobs(ctx, abstractSearchJobs(
		req,
		targets,
		s.networkName,
		s.perPeerTimeout,
	))
	abstracts := make(map[yagomodel.Hash]map[yagomodel.Hash]struct{}, len(targets))
	successes := make(map[yagomodel.Hash]int, len(targets))
	var failures []searchcore.PartialFailure
	for _, result := range results {
		if result.err != nil {
			failures = append(failures, peerFailure(result.peer, result.err))
			continue
		}
		successes[result.term]++
		urls, err := yagomodel.DecodeSearchIndexAbstract(
			result.response.IndexAbstract[result.term],
		)
		if err != nil {
			failures = append(failures, peerFailure(result.peer, err))
			continue
		}
		if abstracts[result.term] == nil {
			abstracts[result.term] = map[yagomodel.Hash]struct{}{}
		}
		for _, url := range urls {
			abstracts[result.term][url] = struct{}{}
		}
	}
	for _, target := range targets {
		if successes[target.term] == 0 {
			failures = append(failures, searchcore.PartialFailure{
				Source: "remote-yacy",
				Reason: fmt.Sprintf("no index abstract responses for %s", target.term),
			})
		}
	}

	return abstracts, failures
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
	target, err := peer.HTTPEndpoint(yagoproto.PathSearch)
	if err != nil {
		return yagoproto.SearchResponse{}, fmt.Errorf("%w: target: %w", errRemoteSearchFailed, err)
	}
	target.RawQuery = searchReq.Form().Encode()

	httpReq, err := newRemoteSearchRequest(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return yagoproto.SearchResponse{}, fmt.Errorf("%w: request: %w", errRemoteSearchFailed, err)
	}
	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		return yagoproto.SearchResponse{}, fmt.Errorf("%w: get: %w", errRemoteSearchFailed, err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != http.StatusOK {
		return yagoproto.SearchResponse{}, fmt.Errorf(
			"%w: status %d",
			errRemoteSearchFailed,
			httpResp.StatusCode,
		)
	}

	return readRemoteSearchResponse(httpResp.Body)
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
		hashes = append(hashes, yagomodel.WordHash(term))
	}

	return hashes
}

func readRemoteSearchResponse(body io.Reader) (yagoproto.SearchResponse, error) {
	raw, err := io.ReadAll(io.LimitReader(body, remoteSearchBodyCap+1))
	if err != nil {
		return yagoproto.SearchResponse{}, fmt.Errorf(
			"%w: read response: %w",
			errRemoteSearchFailed,
			err,
		)
	}
	if len(raw) > remoteSearchBodyCap {
		return yagoproto.SearchResponse{}, fmt.Errorf(
			"%w: response too large",
			errRemoteSearchFailed,
		)
	}
	msg, _ := yagomodel.ParseMessage(string(raw))
	parsed, err := yagoproto.ParseSearchResponse(msg)
	if err != nil {
		return yagoproto.SearchResponse{}, fmt.Errorf(
			"%w: search response: %w",
			errRemoteSearchFailed,
			err,
		)
	}

	return parsed, nil
}

func (s searcher) response(
	ctx context.Context,
	req searchcore.Request,
	results []peerSearchResult,
) searchcore.Response {
	var resp searchcore.Response
	resp.Request = req
	for _, result := range results {
		if result.err != nil {
			resp.PartialFailures = append(
				resp.PartialFailures,
				peerFailure(result.peer, result.err),
			)
			continue
		}
		resp.TotalResults += result.response.JoinCount
		normalized, err := searchResults(ctx, req, result.response.Resources)
		if err != nil {
			resp.PartialFailures = append(resp.PartialFailures, peerFailure(result.peer, err))
			continue
		}
		resp.Results = append(resp.Results, normalized...)
	}
	resp.Results = offsetResults(dedupeSearchResults(resp.Results), req.Offset, req.Limit)

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
		source = "remote-yacy"
	}

	return searchcore.PartialFailure{Source: source, Reason: err.Error()}
}

func searchResults(
	ctx context.Context,
	req searchcore.Request,
	rows []yagomodel.URIMetadataRow,
) ([]searchcore.Result, error) {
	results := make([]searchcore.Result, 0, len(rows))
	for i, row := range rows {
		result, err := searchResult(ctx, req, row, i, len(rows))
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, nil
}

func searchResult(
	ctx context.Context,
	req searchcore.Request,
	row yagomodel.URIMetadataRow,
	rank int,
	total int,
) (searchcore.Result, error) {
	rawURL, err := decodedMetadataProperty(ctx, row, yagomodel.URLMetaURL)
	if err != nil {
		return searchcore.Result{}, err
	}
	title, err := decodedMetadataProperty(ctx, row, yagomodel.URLMetaColDescription)
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

	return searchcore.Result{
		Title:         title,
		URL:           rawURL,
		DisplayURL:    displayURL(host, pathValue),
		Snippet:       title,
		Score:         float64(total-rank) * 0.5,
		Source:        searchcore.SourceRemote,
		Host:          host,
		Path:          pathValue,
		File:          file,
		URLHash:       hash.String(),
		Size:          metadataSize(row),
		Date:          row.Freshness(),
		ContentDomain: req.ContentDomain,
		Language:      req.Language,
	}, nil
}

func decodedMetadataProperty(
	ctx context.Context,
	row yagomodel.URIMetadataRow,
	key string,
) (string, error) {
	raw := row.Properties[key]
	if raw == "" {
		return "", nil
	}
	value, err := yagomodel.DecodeWireForm(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("decode url metadata %s: %w", key, err)
	}

	return value, nil
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
