package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
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

type inboundMissingScript struct {
	urls []yagomodel.Hash
	err  error
}

func (s inboundMissingScript) MissingURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return s.urls, s.err
}

type inboundReferencesScript struct {
	urls []yagomodel.Hash
	err  error
}

func (s inboundReferencesScript) ReferencedURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return s.urls, s.err
}

func TestObserveDHTInboundStorageKeepsStorageWithoutObserver(t *testing.T) {
	storage := nodeStorage{postingReceiver: &inboundPostingReceiverScript{}}

	got := observeDHTInboundStorage(storage, nil, nil)

	if got.postingReceiver != storage.postingReceiver {
		t.Fatal("posting receiver changed without observer")
	}
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
		next:       next,
		missing:    inboundMissingScript{urls: []yagomodel.Hash{first, second}},
		references: inboundReferencesScript{urls: []yagomodel.Hash{first, second}},
		observer:   observer,
	}

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

func TestDHTInboundURLReceiverSkipsReconciliationWhenPrecheckFails(t *testing.T) {
	for _, tc := range []struct {
		name       string
		missing    inboundURLMissingChecker
		references inboundURLReferenceMatcher
	}{
		{
			name:       "missing checker absent",
			references: inboundReferencesScript{urls: []yagomodel.Hash{yagomodel.WordHash("u1")}},
		},
		{
			name:       "missing checker fails",
			missing:    inboundMissingScript{err: errors.New("missing failed")},
			references: inboundReferencesScript{urls: []yagomodel.Hash{yagomodel.WordHash("u1")}},
		},
		{
			name:       "reference matcher fails",
			missing:    inboundMissingScript{urls: []yagomodel.Hash{yagomodel.WordHash("u1")}},
			references: inboundReferencesScript{err: errors.New("references failed")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry, observer := inboundMetrics(t)
			receiver := dhtInboundURLReceiver{
				next:       &inboundURLReceiverScript{},
				missing:    tc.missing,
				references: tc.references,
				observer:   observer,
			}

			if _, err := receiver.Receive(
				context.Background(),
				[]yagomodel.URIMetadataRow{inboundMetadataRow(yagomodel.WordHash("u1"))},
			); err != nil {
				t.Fatalf("Receive: %v", err)
			}
			assertMetric(t, registry, "yacy_url_metadata_reconciled_total", 0)
		})
	}
}

func inboundMetrics(t *testing.T) (*prometheus.Registry, *metrics.DHTInboundMetrics) {
	t.Helper()

	registry := prometheus.NewRegistry()

	return registry, metrics.NewDHTInboundMetrics(registry)
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
