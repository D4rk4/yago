package yagonode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

type inboundPostingReceiverScript struct {
	receipt rwi.Receipt
	err     error
	got     []yagomodel.RWIPosting
}

func (s *inboundPostingReceiverScript) Receive(
	_ context.Context,
	entries []yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	s.got = append([]yagomodel.RWIPosting(nil), entries...)

	return s.receipt, s.err
}

type inboundURLReceiverScript struct {
	receipt urlmeta.Receipt
	err     error
	got     []yagomodel.URIMetadataRow
}

func (s *inboundURLReceiverScript) Receive(
	_ context.Context,
	rows []yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	s.got = append([]yagomodel.URIMetadataRow(nil), rows...)

	return s.receipt, s.err
}

func TestObserveDHTInboundStorageKeepsStorageWithoutObserver(t *testing.T) {
	storage := nodeStorage{postingReceiver: &inboundPostingReceiverScript{}}

	got := observeDHTInboundStorage(storage, nil, nil)

	if got.postingReceiver != storage.postingReceiver {
		t.Fatal("posting receiver changed without observer")
	}
}

func TestAssembleNodeScopesDHTInboundObservationToWireTransfers(t *testing.T) {
	assembled, localStorage, tally, registry := assembleDHTInboundScopeTestNode(t)
	assertLocalIngestDoesNotCountDHT(t, localStorage, tally, registry)
	assertWireTransfersCountDHT(t, assembled, tally, registry)
}

func assembleDHTInboundScopeTestNode(
	t *testing.T,
) (node, nodeStorage, *transfertally.Tally, *prometheus.Registry) {
	t.Helper()
	restoreAssemblySeams(t)
	assembleRuntimePeerExchange = func(peerExchange) (peerExchangeRuntime, error) {
		return peerExchangeRuntime{announcer: fakeAnnouncer{}}, nil
	}
	var localStorage nodeStorage
	buildRuntimeCrawl = func(
		_ context.Context,
		_ crawlConfig,
		_ nodeidentity.Identity,
		storage nodeStorage,
		_ *vault.Vault,
	) (crawlProcess, error) {
		localStorage = storage

		return nil, nil
	}
	var tally *transfertally.Tally
	openRuntimeTransferTally = func(storageVault *vault.Vault) (*transfertally.Tally, error) {
		opened, err := transfertally.Open(storageVault)
		if err != nil {
			return nil, fmt.Errorf("open transfer tally: %w", err)
		}
		tally = opened

		return opened, nil
	}
	registry, observer := inboundMetrics(t)
	config := testConfig(t)
	config.AdvertiseRemoteIndex = true
	storagePressure := yagocrawlcontract.NewStoragePressureGate(
		config.DataDir,
		yagocrawlcontract.StoragePressurePolicy{},
	)
	assembled, err := assembleNode(
		t.Context(),
		config,
		openTestVault(t),
		http.DefaultClient,
		nodeTelemetry{
			dhtOutbound:     metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:      observer,
			storagePressure: storagePressure,
		},
	)
	if err != nil {
		t.Fatalf("assemble node: %v", err)
	}

	return assembled, localStorage, tally, registry
}

func assertLocalIngestDoesNotCountDHT(
	t *testing.T,
	localStorage nodeStorage,
	tally *transfertally.Tally,
	registry *prometheus.Registry,
) {
	t.Helper()
	localURL := yagomodel.Hash("LOCALURL0001")
	if _, err := localStorage.urlReceiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{dhtOutboundURLRow(localURL)},
	); err != nil {
		t.Fatalf("local URL ingest: %v", err)
	}
	if _, err := localStorage.postingReceiver.Receive(
		t.Context(),
		[]yagomodel.RWIPosting{
			dhtOutboundPosting(yagomodel.Hash("LOCALWORD001"), localURL),
		},
	); err != nil {
		t.Fatalf("local RWI ingest: %v", err)
	}
	assertInboundTransferTotals(t, tally, 0, 0)
	assertMetric(t, registry, "yacy_rwi_received_postings_total", 0)
	assertMetric(t, registry, "yacy_url_metadata_received_total", 0)
}

func assertWireTransfersCountDHT(
	t *testing.T,
	assembled node,
	tally *transfertally.Tally,
	registry *prometheus.Registry,
) {
	t.Helper()
	wireURL := yagomodel.Hash("WIREURL00001")
	serveWireTransfer(t, assembled.peerMux, yagoproto.PathTransferURL, yagoproto.TransferURLRequest{
		NetworkName: assembled.identity.NetworkName,
		Iam:         yagomodel.Hash("REMOTEPEER01"),
		YouAre:      assembled.identity.Hash,
		URLCount:    1,
		URLs:        []yagomodel.URIMetadataRow{dhtOutboundURLRow(wireURL)},
	}.Form().Encode())
	serveWireTransfer(t, assembled.peerMux, yagoproto.PathTransferRWI, yagoproto.TransferRWIRequest{
		NetworkName: assembled.identity.NetworkName,
		Iam:         yagomodel.Hash("REMOTEPEER01"),
		YouAre:      assembled.identity.Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes: []yagomodel.RWIPosting{
			dhtOutboundPosting(yagomodel.Hash("WIREWORD0001"), wireURL),
		},
	}.Form().Encode())
	assertInboundTransferTotals(t, tally, 1, 1)
	assertMetric(t, registry, "yacy_rwi_received_postings_total", 1)
	assertMetric(t, registry, "yacy_url_metadata_received_total", 1)
}

func TestDHTInboundPostingReceiverCountsAcceptedRows(t *testing.T) {
	registry, observer := inboundMetrics(t)
	times := []time.Time{
		time.Unix(10, 0),
		time.Unix(12, 0),
	}
	next := &inboundPostingReceiverScript{
		receipt: rwi.Receipt{UnknownURL: []yagomodel.Hash{yagomodel.WordHash("u1")}},
	}
	receiver := dhtInboundPostingReceiver{
		next:     next,
		observer: observer,
		now: func() time.Time {
			nextTime := times[0]
			times = times[1:]

			return nextTime
		},
	}

	receipt, err := receiver.Receive(context.Background(), make([]yagomodel.RWIPosting, 3))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(next.got) != 3 || len(receipt.UnknownURL) != 1 {
		t.Fatalf("receipt/next = %#v/%d", receipt, len(next.got))
	}
	assertMetric(t, registry, "yacy_rwi_received_postings_total", 3)
	assertMetric(t, registry, "yacy_rwi_rejected_postings_total", 0)
	assertMetric(t, registry, "yacy_rwi_unknown_url_total", 1)
}

func TestDHTInboundPostingReceiverCountsRejectedRows(t *testing.T) {
	registry, observer := inboundMetrics(t)
	busy := dhtInboundPostingReceiver{
		next:     &inboundPostingReceiverScript{receipt: rwi.Receipt{Busy: true}},
		observer: observer,
		now:      time.Now,
	}
	if _, err := busy.Receive(context.Background(), make([]yagomodel.RWIPosting, 2)); err != nil {
		t.Fatalf("busy Receive: %v", err)
	}
	assertMetric(t, registry, "yacy_rwi_rejected_postings_total", 2)

	want := errors.New("receive failed")
	failing := dhtInboundPostingReceiver{
		next:     &inboundPostingReceiverScript{err: want},
		observer: observer,
		now:      time.Now,
	}
	if _, err := failing.Receive(
		context.Background(),
		make([]yagomodel.RWIPosting, 4),
	); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	assertMetric(t, registry, "yacy_rwi_rejected_postings_total", 6)
}

func TestDHTInboundURLReceiverCountsRows(t *testing.T) {
	registry, observer := inboundMetrics(t)
	first := yagomodel.WordHash("u1")
	second := yagomodel.WordHash("u2")
	next := &inboundURLReceiverScript{
		receipt: urlmeta.Receipt{ErrorURL: []yagomodel.Hash{second}},
	}
	receiver := dhtInboundURLReceiver{
		next:           next,
		observer:       observer,
		reconciliation: newDHTInboundReconciliation(2),
	}
	receiver.reconciliation.note([]yagomodel.Hash{first, second})

	receipt, err := receiver.Receive(context.Background(), []yagomodel.URIMetadataRow{
		inboundMetadataRow(first),
		inboundMetadataRow(second),
		{},
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(next.got) != 3 || len(receipt.ErrorURL) != 1 {
		t.Fatalf("receipt/next = %#v/%d", receipt, len(next.got))
	}
	assertMetric(t, registry, "yacy_url_metadata_received_total", 1)
	assertMetric(t, registry, "yacy_url_metadata_rejected_total", 2)
	assertMetric(t, registry, "yacy_url_metadata_reconciled_total", 1)
}

func TestDHTInboundURLReceiverDoesNotReconcileDuplicateRows(t *testing.T) {
	registry, observer := inboundMetrics(t)
	hash := yagomodel.WordHash("u1")
	reconciliation := newDHTInboundReconciliation(1)
	reconciliation.note([]yagomodel.Hash{hash})
	receiver := dhtInboundURLReceiver{
		next: &inboundURLReceiverScript{receipt: urlmeta.Receipt{
			Double:      1,
			ExistingURL: []yagomodel.Hash{hash},
		}},
		observer:       observer,
		reconciliation: reconciliation,
	}

	if _, err := receiver.Receive(
		context.Background(),
		[]yagomodel.URIMetadataRow{inboundMetadataRow(hash)},
	); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	assertMetric(t, registry, "yacy_url_metadata_reconciled_total", 0)
	if got := reconciliation.resolve(
		[]yagomodel.URIMetadataRow{inboundMetadataRow(hash)},
		nil,
		nil,
	); got != 0 {
		t.Fatalf("released duplicate reconciled later = %d, want zero", got)
	}
}

func TestDHTInboundURLReceiverCountsRejectedRows(t *testing.T) {
	registry, observer := inboundMetrics(t)
	busy := dhtInboundURLReceiver{
		next:     &inboundURLReceiverScript{receipt: urlmeta.Receipt{Busy: true}},
		observer: observer,
	}
	if _, err := busy.Receive(
		context.Background(),
		[]yagomodel.URIMetadataRow{{}, {}},
	); err != nil {
		t.Fatalf("busy Receive: %v", err)
	}
	assertMetric(t, registry, "yacy_url_metadata_rejected_total", 2)

	want := errors.New("url receive failed")
	failing := dhtInboundURLReceiver{
		next:     &inboundURLReceiverScript{err: want},
		observer: observer,
	}
	if _, err := failing.Receive(
		context.Background(),
		[]yagomodel.URIMetadataRow{{}},
	); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	assertMetric(t, registry, "yacy_url_metadata_rejected_total", 3)
}

func inboundMetrics(t *testing.T) (*prometheus.Registry, *metrics.DHTInboundMetrics) {
	t.Helper()

	registry := prometheus.NewRegistry()

	return registry, metrics.NewDHTInboundMetrics(registry)
}

func assertInboundTransferTotals(
	t *testing.T,
	tally *transfertally.Tally,
	wantWords int64,
	wantURLs int64,
) {
	t.Helper()

	totals, err := tally.Totals(t.Context())
	if err != nil {
		t.Fatalf("transfer totals: %v", err)
	}
	if totals.ReceivedWords != wantWords || totals.ReceivedURLs != wantURLs {
		t.Fatalf(
			"received transfer totals words/URLs = %d/%d, want %d/%d",
			totals.ReceivedWords,
			totals.ReceivedURLs,
			wantWords,
			wantURLs,
		)
	}
}

func serveWireTransfer(
	t *testing.T,
	handler http.Handler,
	path string,
	encodedForm string,
) {
	t.Helper()

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		path,
		strings.NewReader(encodedForm),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("POST %s status = %d, want 200: %s", path, response.Code, response.Body.String())
	}
}

func assertMetric(
	t *testing.T,
	registry *prometheus.Registry,
	name string,
	want float64,
) {
	t.Helper()

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		var got float64
		for _, metric := range family.GetMetric() {
			got += metric.GetCounter().GetValue()
		}
		if got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}

		return
	}

	t.Fatalf("%s not found", name)
}

func inboundMetadataRow(hash yagomodel.Hash) yagomodel.URIMetadataRow {
	return yagomodel.URIMetadataRow{
		Properties: map[string]string{yagomodel.URLMetaHash: hash.String()},
	}
}
