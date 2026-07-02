package searchremote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/dhttarget"
	"github.com/D4rk4/yago/yacynode/internal/searchcore"
	"github.com/D4rk4/yago/yacyproto"
)

const (
	DefaultMaxPeers           = 8
	DefaultRedundancy         = 3
	DefaultConcurrency        = 4
	DefaultPerPeerTimeout     = time.Second
	DefaultOverallTimeout     = 2 * time.Second
	DefaultMinimumPeerAgeDays = 3
	maxPartitionExponent      = 8
	remoteSearchBodyCap       = 512 << 10
)

var errRemoteSearchFailed = errors.New("remote search failed")

type PeerSource interface {
	ReachablePeers(ctx context.Context) []yacymodel.Seed
}

type Config struct {
	Client             *http.Client
	NetworkName        string
	Peers              PeerSource
	MaxPeers           int
	Redundancy         int
	MinimumPeerAgeDays int
	PartitionExponent  int
	Concurrency        int
	PerPeerTimeout     time.Duration
	OverallTimeout     time.Duration
}

type searcher struct {
	client             *http.Client
	networkName        string
	peers              PeerSource
	maxPeers           int
	redundancy         int
	minimumPeerAgeDays int
	partitionExponent  int
	concurrency        int
	perPeerTimeout     time.Duration
	overallTimeout     time.Duration
}

type peerSearchResult struct {
	peer     yacymodel.Seed
	response yacyproto.SearchResponse
	err      error
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
		partitionExponent:  normalizedPartitionExponent(config.PartitionExponent),
		concurrency:        positiveOrDefault(config.Concurrency, DefaultConcurrency),
		perPeerTimeout:     durationOrDefault(config.PerPeerTimeout, DefaultPerPeerTimeout),
		overallTimeout:     durationOrDefault(config.OverallTimeout, DefaultOverallTimeout),
	}
}

func (s searcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	ctx, cancel := s.overallContext(ctx)
	defer cancel()

	peers, noPeersReason := s.remotePeers(ctx, req)
	if len(peers) == 0 {
		return searchcore.Response{
			Request: req,
			PartialFailures: []searchcore.PartialFailure{{
				Source: "remote-yacy",
				Reason: noPeersReason,
			}},
		}, nil
	}

	results := s.queryPeers(ctx, peers, req)

	return s.response(ctx, req, results), nil
}

func (s searcher) overallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.overallTimeout)
}

func (s searcher) remotePeers(
	ctx context.Context,
	req searchcore.Request,
) ([]yacymodel.Seed, string) {
	if s.peers == nil {
		return nil, "no reachable peers"
	}
	peers := s.peers.ReachablePeers(ctx)
	if len(peers) == 0 {
		return nil, "no reachable peers"
	}

	hashes := termHashes(req.Terms)
	if len(hashes) == 0 {
		return nil, "no query terms"
	}

	selected := selectDHTSearchPeers(hashes, peers, dhtSearchPeerConfig{
		maxPeers:           s.maxPeers,
		redundancy:         s.redundancy,
		minimumPeerAgeDays: s.minimumPeerAgeDays,
		partitionExponent:  s.partitionExponent,
	})
	if len(selected) == 0 {
		return nil, "no dht search targets"
	}

	return selected, ""
}

type dhtSearchPeerConfig struct {
	maxPeers           int
	redundancy         int
	minimumPeerAgeDays int
	partitionExponent  int
}

func selectDHTSearchPeers(
	hashes []yacymodel.Hash,
	peers []yacymodel.Seed,
	config dhtSearchPeerConfig,
) []yacymodel.Seed {
	selected := make([]yacymodel.Seed, 0, config.maxPeers)
	seen := make(map[yacymodel.Hash]struct{})
	partitions := 1 << config.partitionExponent
	for _, hash := range hashes {
		for partition := range partitions {
			position, err := yacymodel.VerticalPosition(
				hash,
				uint64(partition),
				config.partitionExponent,
			)
			if err != nil {
				continue
			}
			targets, _ := dhttarget.SelectAtPosition(position, peers, dhttarget.Config{
				Redundancy:     config.redundancy,
				MinimumAgeDays: config.minimumPeerAgeDays,
			})
			for _, target := range targets {
				if _, ok := seen[target.Peer.Hash]; ok {
					continue
				}
				seen[target.Peer.Hash] = struct{}{}
				selected = append(selected, target.Peer)
				if len(selected) >= config.maxPeers {
					return selected
				}
			}
		}
	}

	return selected
}

func (s searcher) queryPeers(
	ctx context.Context,
	peers []yacymodel.Seed,
	req searchcore.Request,
) []peerSearchResult {
	workerCount := min(s.concurrency, len(peers))
	jobs := make(chan yacymodel.Seed)
	results := make(chan peerSearchResult, len(peers))
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for peer := range jobs {
				results <- s.queryPeer(ctx, peer, req)
			}
		}()
	}
	for _, peer := range peers {
		jobs <- peer
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]peerSearchResult, 0, len(peers))
	for result := range results {
		out = append(out, result)
	}

	return out
}

func (s searcher) queryPeer(
	ctx context.Context,
	peer yacymodel.Seed,
	req searchcore.Request,
) peerSearchResult {
	peerCtx, cancel := context.WithTimeout(ctx, s.perPeerTimeout)
	defer cancel()

	resp, err := s.remoteSearch(peerCtx, peer, req)
	return peerSearchResult{peer: peer, response: resp, err: err}
}

func (s searcher) remoteSearch(
	ctx context.Context,
	peer yacymodel.Seed,
	req searchcore.Request,
) (yacyproto.SearchResponse, error) {
	target, err := peer.HTTPEndpoint(yacyproto.PathSearch)
	if err != nil {
		return yacyproto.SearchResponse{}, fmt.Errorf("%w: target: %w", errRemoteSearchFailed, err)
	}
	searchReq := remoteSearchRequest(req, s.networkName, s.perPeerTimeout)
	target.RawQuery = searchReq.Form().Encode()

	httpReq, err := newRemoteSearchRequest(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return yacyproto.SearchResponse{}, fmt.Errorf("%w: request: %w", errRemoteSearchFailed, err)
	}
	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		return yacyproto.SearchResponse{}, fmt.Errorf("%w: get: %w", errRemoteSearchFailed, err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != http.StatusOK {
		return yacyproto.SearchResponse{}, fmt.Errorf(
			"%w: status %d",
			errRemoteSearchFailed,
			httpResp.StatusCode,
		)
	}

	return readRemoteSearchResponse(httpResp.Body)
}

func remoteSearchRequest(
	req searchcore.Request,
	networkName string,
	perPeerTimeout time.Duration,
) yacyproto.SearchRequest {
	return yacyproto.SearchRequest{
		NetworkName: networkName,
		Query:       termHashes(req.Terms),
		Exclude:     termHashes(req.ExcludedTerms),
		Count:       req.Limit,
		Time:        int(perPeerTimeout / time.Millisecond),
		ContentDom:  yacyproto.SearchContentDomain(req.ContentDomain),
		Language:    req.Language,
		Prefer:      req.PreferMaskFilter,
		Filter:      req.URLMaskFilter,
		SiteHost:    req.SiteHost,
		FileType:    req.FileType,
	}
}

func termHashes(terms []string) []yacymodel.Hash {
	hashes := make([]yacymodel.Hash, 0, len(terms))
	for _, term := range terms {
		hashes = append(hashes, yacymodel.WordHash(term))
	}

	return hashes
}

func readRemoteSearchResponse(body io.Reader) (yacyproto.SearchResponse, error) {
	raw, err := io.ReadAll(io.LimitReader(body, remoteSearchBodyCap+1))
	if err != nil {
		return yacyproto.SearchResponse{}, fmt.Errorf(
			"%w: read response: %w",
			errRemoteSearchFailed,
			err,
		)
	}
	if len(raw) > remoteSearchBodyCap {
		return yacyproto.SearchResponse{}, fmt.Errorf(
			"%w: response too large",
			errRemoteSearchFailed,
		)
	}
	msg, _ := yacymodel.ParseMessage(string(raw))
	parsed, err := yacyproto.ParseSearchResponse(msg)
	if err != nil {
		return yacyproto.SearchResponse{}, fmt.Errorf(
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
	resp.Results = offsetResults(resp.Results, req.Offset, req.Limit)

	return resp
}

func peerFailure(peer yacymodel.Seed, err error) searchcore.PartialFailure {
	source := peer.Hash.String()
	if source == "" {
		source = "remote-yacy"
	}

	return searchcore.PartialFailure{Source: source, Reason: err.Error()}
}

func searchResults(
	ctx context.Context,
	req searchcore.Request,
	rows []yacymodel.URIMetadataRow,
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
	row yacymodel.URIMetadataRow,
	rank int,
	total int,
) (searchcore.Result, error) {
	rawURL, err := decodedMetadataProperty(ctx, row, yacymodel.URLMetaURL)
	if err != nil {
		return searchcore.Result{}, err
	}
	title, err := decodedMetadataProperty(ctx, row, yacymodel.URLMetaColDescription)
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
	row yacymodel.URIMetadataRow,
	key string,
) (string, error) {
	raw := row.Properties[key]
	if raw == "" {
		return "", nil
	}
	value, err := yacymodel.DecodeWireForm(ctx, raw)
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

func metadataSize(row yacymodel.URIMetadataRow) int {
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

func normalizedPartitionExponent(value int) int {
	if value < 0 {
		return 0
	}
	if value > maxPartitionExponent {
		return maxPartitionExponent
	}

	return value
}
