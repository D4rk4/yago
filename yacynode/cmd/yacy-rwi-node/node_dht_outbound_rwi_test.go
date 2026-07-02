package main

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
	"github.com/D4rk4/yago/yacynode/internal/metrics"
	"github.com/D4rk4/yago/yacynode/internal/nodestatus"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
)

type outboundPostingStoreScript struct {
	count        int
	words        []yacymodel.WordPostings
	selectConfig rwi.OutboundSelectionConfig
	selectCalls  int
	restoreCalls int
	restoreWords []yacymodel.WordPostings
	selectErr    error
	restoreErr   error
}

func (s *outboundPostingStoreScript) RWICount(context.Context) (int, error) {
	return s.count, nil
}

func (s *outboundPostingStoreScript) ScanWord(
	context.Context,
	yacymodel.Hash,
	func(yacymodel.RWIPosting) (bool, error),
) error {
	return nil
}

func (s *outboundPostingStoreScript) SelectOutbound(
	_ context.Context,
	config rwi.OutboundSelectionConfig,
) (rwi.OutboundSelection, error) {
	s.selectCalls++
	s.selectConfig = config
	if s.selectErr != nil {
		return rwi.OutboundSelection{}, s.selectErr
	}

	return rwi.OutboundSelection{Words: s.words}, nil
}

func (s *outboundPostingStoreScript) RestoreOutbound(
	_ context.Context,
	words []yacymodel.WordPostings,
) (int, error) {
	s.restoreCalls++
	s.restoreWords = words
	if s.restoreErr != nil {
		return 0, s.restoreErr
	}

	count := 0
	for _, word := range words {
		count += len(word.Postings)
	}

	return count, nil
}

type urlDirectoryScript struct {
	missing []yacymodel.Hash
	count   int
}

func (d urlDirectoryScript) RowsByHash(
	context.Context,
	[]yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	return nil, nil
}

func (d urlDirectoryScript) MissingURLs(
	context.Context,
	[]yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	return d.missing, nil
}

func (d urlDirectoryScript) Count(context.Context) (int, error) {
	return d.count, nil
}

func TestDHTOutboundRWIWordsAdaptsSelectionAndRestore(t *testing.T) {
	t.Parallel()

	word := yacymodel.Hash("AAAAAAAAAAAA")
	url := yacymodel.Hash("BBBBBBBBBBBB")
	source := &outboundPostingStoreScript{words: []yacymodel.WordPostings{{
		WordHash: word,
		Postings: []yacymodel.RWIPosting{dhtOutboundPosting(word, url)},
	}}}
	adapter := dhtOutboundRWIWords{postings: source}

	words, err := adapter.SelectOutboundWords(context.Background(), 2, 3)
	if err != nil {
		t.Fatalf("SelectOutboundWords: %v", err)
	}
	if len(words) != 1 ||
		source.selectConfig.MaxWords != 2 ||
		source.selectConfig.MaxPostings != 3 {
		t.Fatalf("words/source = %#v/%#v", words, source)
	}

	restored, err := adapter.RestoreOutboundWords(context.Background(), words)
	if err != nil {
		t.Fatalf("RestoreOutboundWords: %v", err)
	}
	if restored != 1 || source.restoreCalls != 1 {
		t.Fatalf("restored/source = %d/%#v", restored, source)
	}
}

func TestDHTOutboundRWIWordsReturnsStoreErrors(t *testing.T) {
	t.Parallel()

	selectErr := errors.New("select failed")
	_, err := (dhtOutboundRWIWords{
		postings: &outboundPostingStoreScript{selectErr: selectErr},
	}).SelectOutboundWords(context.Background(), 1, 1)
	if !errors.Is(err, selectErr) {
		t.Fatalf("select error = %v, want %v", err, selectErr)
	}

	restoreErr := errors.New("restore failed")
	_, err = (dhtOutboundRWIWords{
		postings: &outboundPostingStoreScript{restoreErr: restoreErr},
	}).RestoreOutboundWords(context.Background(), []yacymodel.WordPostings{{}})
	if !errors.Is(err, restoreErr) {
		t.Fatalf("restore error = %v, want %v", err, restoreErr)
	}
}

func TestBuildDHTOutboundRuntimeSkipsFeederWhenGatesAreClosed(t *testing.T) {
	t.Parallel()

	word := yacymodel.Hash("CCCCCCCCCCCC")
	url := yacymodel.Hash("DDDDDDDDDDDD")
	postings := &outboundPostingStoreScript{
		count: 1,
		words: []yacymodel.WordPostings{{
			WordHash: word,
			Postings: []yacymodel.RWIPosting{dhtOutboundPosting(word, url)},
		}},
	}
	config := testConfig(t)
	config.DHT.Gates.MinimumConnectedPeer = 2
	config.DHT.Gates.MinimumRWIWord = 1
	metadata := urlDirectoryScript{count: 1}
	report := nodestatus.NewReport(nodeIdentity(config), postings, metadata)
	process := buildDHTOutboundRuntime(dhtOutboundRuntimeAssembly{
		ctx:     context.Background(),
		config:  config,
		storage: openTestVault(t),
		nodeStorage: nodeStorage{
			urlDirectory:     metadata,
			postings:         postings,
			outboundPostings: postings,
		},
		report: report,
		roster: reachableRoster{peers: []yacymodel.Seed{dhtOutboundPeer(t)}},
		client: http.DefaultClient,
		observer: metrics.NewDHTOutboundMetrics(
			prometheus.NewRegistry(),
		),
		reachability: &publicReachabilityScript{reachable: true},
	})

	receipt, err := process.cycle.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if receipt.Feed.State != "" ||
		receipt.Distribution.State != dhtexchange.DistributionGateClosed ||
		postings.selectCalls != 0 {
		t.Fatalf("receipt/postings = %#v/%#v", receipt, postings)
	}
}

func dhtOutboundPosting(word, url yacymodel.Hash) yacymodel.RWIPosting {
	return yacymodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash: url.String(),
		},
	}
}

func dhtOutboundPeer(tb testing.TB) yacymodel.Seed {
	tb.Helper()

	host, err := yacymodel.ParseHost("192.0.2.1")
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}

	return yacymodel.Seed{
		Hash: yacymodel.Hash("EEEEEEEEEEEE"),
		IP:   yacymodel.Some(host),
		Port: yacymodel.Some(yacymodel.Port(8090)),
		Flags: yacymodel.Some(
			yacymodel.ZeroFlags().Set(yacymodel.FlagAcceptRemoteIndex, true),
		),
	}
}

var _ urlmeta.URLDirectory = urlDirectoryScript{}
