package yagonode

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
)

type dhtOutboundTransferFixture struct {
	receiverHash   yagomodel.Hash
	receiverServer *httptest.Server
	word           yagomodel.Hash
	urlHash        yagomodel.Hash
}

func TestOutboundDHTTransfersStoredRWIAndURLToPeer(t *testing.T) {
	ctx := context.Background()
	receiverHash := yagomodel.Hash("BBBBBBBBBBBB")
	receiverConfig := testConfig(t)
	receiverConfig.Hash = receiverHash
	receiverConfig.Name = "receiver"
	receiverStorage, receiverServer := startDHTReceiverNode(t, receiverConfig)

	word := yagomodel.Hash("CCCCCCCCCCCC")
	urlHash := yagomodel.Hash("DDDDDDDDDDDD")
	senderStorage, process := startDHTSenderRuntime(
		t,
		ctx,
		dhtOutboundTransferFixture{
			receiverHash:   receiverHash,
			receiverServer: receiverServer,
			word:           word,
			urlHash:        urlHash,
		},
	)

	receipt, err := process.cycle.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	assertDHTTransferReceipt(t, receipt, urlHash)
	assertDHTTransferCounts(t, ctx, senderStorage, receiverStorage)
	assertDHTReceiverPosting(t, ctx, receiverStorage, word, urlHash)
}

func TestOpenNodeStorageRecoversPendingOutboundSelectionAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	word := yagomodel.Hash("CCCCCCCCCCCC")
	urlHash := yagomodel.Hash("DDDDDDDDDDDD")

	firstVault, err := openRuntimeVault(path, 0)
	if err != nil {
		t.Fatalf("open first vault: %v", err)
	}
	firstStorage, err := openNodeStorage(firstVault, "")
	if err != nil {
		t.Fatalf("open first storage: %v", err)
	}
	storeSenderDHTRows(t, ctx, firstStorage, word, urlHash)
	selection, err := firstStorage.outboundPostings.SelectOutbound(
		ctx,
		rwi.OutboundSelectionConfig{},
	)
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	if selection.PostingCount() != 1 {
		t.Fatalf("selection count = %d, want 1", selection.PostingCount())
	}
	if err := firstVault.Close(); err != nil {
		t.Fatalf("close first vault: %v", err)
	}

	reopenedVault, err := openRuntimeVault(path, 0)
	if err != nil {
		t.Fatalf("open reopened vault: %v", err)
	}
	t.Cleanup(func() {
		if err := reopenedVault.Close(); err != nil {
			t.Fatalf("close reopened vault: %v", err)
		}
	})
	recoveredStorage, err := openNodeStorage(reopenedVault, "")
	if err != nil {
		t.Fatalf("open recovered storage: %v", err)
	}
	count, err := recoveredStorage.postings.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if count != 1 {
		t.Fatalf("recovered rwi count = %d, want 1", count)
	}
	assertDHTReceiverPosting(t, ctx, recoveredStorage, word, urlHash)
}

func startDHTSenderRuntime(
	t *testing.T,
	ctx context.Context,
	fixture dhtOutboundTransferFixture,
) (nodeStorage, dhtOutboundProcess) {
	t.Helper()

	senderConfig := testConfig(t)
	senderConfig.Hash = yagomodel.Hash("AAAAAAAAAAAA")
	senderConfig.Name = "sender"
	senderConfig.DHT.Gates.MinimumConnectedPeer = 1
	senderConfig.DHT.Gates.MinimumRWIWord = 1
	senderVault := openTestVault(t)
	senderStorage, err := openNodeStorage(senderVault, "")
	if err != nil {
		t.Fatalf("open sender storage: %v", err)
	}

	storeSenderDHTRows(t, ctx, senderStorage, fixture.word, fixture.urlHash)

	receiverSeed := dhtOutboundServerSeed(t, fixture.receiverHash, fixture.receiverServer)
	report := nodestatus.NewReport(nodeIdentity(senderConfig), nodestatus.ReportSources{
		RWI:       senderStorage.postings,
		URLs:      senderStorage.urlDirectory,
		Peers:     fakeRoster{},
		News:      fakeSeedNews{},
		Transfers: fakeTransferTotals{},
	})
	process := buildDHTOutboundRuntime(dhtOutboundRuntimeAssembly{
		ctx:         ctx,
		config:      senderConfig,
		storage:     senderVault,
		nodeStorage: senderStorage,
		report:      report,
		roster:      reachableRoster{peers: []yagomodel.Seed{receiverSeed}},
		client:      fixture.receiverServer.Client(),
		observer:    metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
		reachability: &publicReachabilityScript{
			reachable: true,
		},
	})

	return senderStorage, process
}

func storeSenderDHTRows(
	t *testing.T,
	ctx context.Context,
	storage nodeStorage,
	word yagomodel.Hash,
	urlHash yagomodel.Hash,
) {
	t.Helper()

	if _, err := storage.urlReceiver.Receive(
		ctx,
		[]yagomodel.URIMetadataRow{dhtOutboundURLRow(urlHash)},
	); err != nil {
		t.Fatalf("store sender url: %v", err)
	}
	if _, err := storage.postingReceiver.Receive(
		ctx,
		[]yagomodel.RWIPosting{dhtOutboundPosting(word, urlHash)},
	); err != nil {
		t.Fatalf("store sender rwi: %v", err)
	}
}

func assertDHTTransferReceipt(
	t *testing.T,
	receipt dhtexchange.ScheduledDistributionReceipt,
	urlHash yagomodel.Hash,
) {
	t.Helper()

	if receipt.Feed.State != dhtexchange.OutboundFeedEnqueued ||
		receipt.Distribution.State != dhtexchange.DistributionSent ||
		receipt.Distribution.Handoff.State != indextransfer.HandoffURLSent ||
		receipt.Distribution.Handoff.SentURLRows != 1 ||
		len(receipt.Distribution.Handoff.RemoteUnknownURL) != 1 ||
		receipt.Distribution.Handoff.RemoteUnknownURL[0] != urlHash {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func assertDHTTransferCounts(
	t *testing.T,
	ctx context.Context,
	senderStorage nodeStorage,
	receiverStorage nodeStorage,
) {
	t.Helper()

	senderCount, err := senderStorage.postings.RWICount(ctx)
	if err != nil {
		t.Fatalf("sender RWICount: %v", err)
	}
	receiverCount, err := receiverStorage.postings.RWICount(ctx)
	if err != nil {
		t.Fatalf("receiver RWICount: %v", err)
	}
	receiverURLCount, err := receiverStorage.urlDirectory.Count(ctx)
	if err != nil {
		t.Fatalf("receiver URL count: %v", err)
	}
	if senderCount != 0 || receiverCount != 1 || receiverURLCount != 1 {
		t.Fatalf(
			"counts sender/receiver/url = %d/%d/%d, want 0/1/1",
			senderCount,
			receiverCount,
			receiverURLCount,
		)
	}
}

func assertDHTReceiverPosting(
	t *testing.T,
	ctx context.Context,
	receiverStorage nodeStorage,
	word yagomodel.Hash,
	urlHash yagomodel.Hash,
) {
	t.Helper()

	seen := 0
	if err := receiverStorage.postings.ScanWord(
		ctx,
		word,
		func(got yagomodel.RWIPosting) (bool, error) {
			seen++
			gotURL, err := got.URLHash()
			if err != nil {
				return false, fmt.Errorf("receiver posting url hash: %w", err)
			}
			if gotURL.Hash() != urlHash {
				t.Fatalf("receiver posting url = %s, want %s", gotURL.Hash(), urlHash)
			}

			return true, nil
		},
	); err != nil {
		t.Fatalf("receiver ScanWord: %v", err)
	}
	if seen != 1 {
		t.Fatalf("receiver postings for word = %d, want 1", seen)
	}
}

func startDHTReceiverNode(
	t *testing.T,
	config nodeConfig,
) (nodeStorage, *httptest.Server) {
	t.Helper()

	storage, err := openNodeStorage(openTestVault(t), "")
	if err != nil {
		t.Fatalf("open receiver storage: %v", err)
	}

	identity := nodeIdentity(config)
	report := nodestatus.NewReport(identity, nodestatus.ReportSources{
		RWI:       storage.postings,
		URLs:      storage.urlDirectory,
		Peers:     fakeRoster{},
		News:      fakeSeedNews{},
		Transfers: fakeTransferTotals{},
	})
	guard := httpguard.NewRequestGuard(
		httpguard.DefaultMaxBodyBytes,
		httpguard.DefaultRequestTimeout,
	)
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(
		mux,
		httpguard.WireGate{
			Guard:   guard,
			Respond: httpguard.NewWireResponder(report),
			Address: httpguard.NewClientAddressResolver(config.TrustedProxies),
		},
	)
	mountNodeProtocol(router, identity, storage, nil, true)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return storage, server
}

func dhtOutboundServerSeed(
	t *testing.T,
	hash yagomodel.Hash,
	server *httptest.Server,
) yagomodel.Seed {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	rawHost, rawPort, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split server host: %v", err)
	}
	host, err := yagomodel.ParseHost(rawHost)
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}
	port, err := yagomodel.ParsePort(rawPort)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	return yagomodel.Seed{
		Hash: hash,
		IP:   yagomodel.Some(host),
		Port: yagomodel.Some(port),
		Flags: yagomodel.Some(
			yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true),
		),
		BirthDate: yagomodel.Some(
			yagomodel.NewSeedBirthDateUTC(time.Now().AddDate(0, 0, -10)),
		),
	}
}

func dhtOutboundURLRow(hash yagomodel.Hash) yagomodel.URIMetadataRow {
	return yagomodel.URIMetadataRow{
		Properties: map[string]string{yagomodel.URLMetaHash: hash.String()},
	}
}
