package yagonode

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type outboundPostingStoreScript struct {
	count        int
	words        []yagomodel.WordPostings
	selectConfig rwi.OutboundSelectionConfig
	selectCalls  int
	restoreCalls int
	confirmCalls int
	recoverCalls int
	restoreWords []yagomodel.WordPostings
	confirmRows  []yagomodel.RWIPosting
	selectErr    error
	restoreErr   error
	confirmErr   error
	recoverErr   error
}

func (s *outboundPostingStoreScript) RWICount(context.Context) (int, error) {
	return s.count, nil
}

func (s *outboundPostingStoreScript) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return 0, nil
}

func (s *outboundPostingStoreScript) ScanWord(
	context.Context,
	yagomodel.Hash,
	func(yagomodel.RWIPosting) (bool, error),
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
	words []yagomodel.WordPostings,
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

func (s *outboundPostingStoreScript) ConfirmOutbound(
	_ context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	s.confirmCalls++
	s.confirmRows = append([]yagomodel.RWIPosting(nil), postings...)
	if s.confirmErr != nil {
		return 0, s.confirmErr
	}

	return len(postings), nil
}

func (s *outboundPostingStoreScript) RecoverOutbound(context.Context) (int, error) {
	s.recoverCalls++
	if s.recoverErr != nil {
		return 0, s.recoverErr
	}

	return 0, nil
}

type urlDirectoryScript struct {
	missing []yagomodel.Hash
	count   int
}

func (d urlDirectoryScript) RowsByHash(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	return nil, nil
}

func (d urlDirectoryScript) MissingURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return d.missing, nil
}

func (d urlDirectoryScript) Count(context.Context) (int, error) {
	return d.count, nil
}

func TestDHTOutboundRWIWordsAdaptsSelectionAndRestore(t *testing.T) {
	t.Parallel()

	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("BBBBBBBBBBBB")
	source := &outboundPostingStoreScript{words: []yagomodel.WordPostings{{
		WordHash: word,
		Postings: []yagomodel.RWIPosting{dhtOutboundPosting(word, url)},
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

	confirmed, err := adapter.ConfirmTransferred(context.Background(), words[0].Postings)
	if err != nil {
		t.Fatalf("ConfirmTransferred: %v", err)
	}
	if confirmed != 1 || source.confirmCalls != 1 {
		t.Fatalf("confirmed/source = %d/%#v", confirmed, source)
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
	}).RestoreOutboundWords(context.Background(), []yagomodel.WordPostings{{}})
	if !errors.Is(err, restoreErr) {
		t.Fatalf("restore error = %v, want %v", err, restoreErr)
	}

	confirmErr := errors.New("confirm failed")
	_, err = (dhtOutboundRWIWords{
		postings: &outboundPostingStoreScript{confirmErr: confirmErr},
	}).ConfirmTransferred(context.Background(), []yagomodel.RWIPosting{{}})
	if !errors.Is(err, confirmErr) {
		t.Fatalf("confirm error = %v, want %v", err, confirmErr)
	}
}

func TestBuildDHTOutboundRuntimeSkipsFeederWhenGatesAreClosed(t *testing.T) {
	t.Parallel()

	word := yagomodel.Hash("CCCCCCCCCCCC")
	url := yagomodel.Hash("DDDDDDDDDDDD")
	postings := &outboundPostingStoreScript{
		count: 1,
		words: []yagomodel.WordPostings{{
			WordHash: word,
			Postings: []yagomodel.RWIPosting{dhtOutboundPosting(word, url)},
		}},
	}
	config := testConfig(t)
	config.DHT.Gates.MinimumConnectedPeer = 2
	config.DHT.Gates.MinimumRWIWord = 1
	metadata := urlDirectoryScript{count: 1}
	report := nodestatus.NewReport(nodeIdentity(config), nodestatus.ReportSources{
		RWI:       postings,
		URLs:      metadata,
		Peers:     fakeRoster{},
		News:      fakeSeedNews{},
		Transfers: fakeTransferTotals{},
	})
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
		roster: reachableRoster{peers: []yagomodel.Seed{dhtOutboundPeer(t)}},
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

func dhtOutboundPosting(word, url yagomodel.Hash) yagomodel.RWIPosting {
	return yagomodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yagomodel.ColURLHash: url.String(),
		},
	}
}

func dhtOutboundPeer(tb testing.TB) yagomodel.Seed {
	tb.Helper()

	host, err := yagomodel.ParseHost("192.0.2.1")
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}

	return yagomodel.Seed{
		Hash: yagomodel.Hash("EEEEEEEEEEEE"),
		IP:   yagomodel.Some(host),
		Port: yagomodel.Some(yagomodel.Port(8090)),
		Flags: yagomodel.Some(
			yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true),
		),
	}
}

var _ urlmeta.URLDirectory = urlDirectoryScript{}
