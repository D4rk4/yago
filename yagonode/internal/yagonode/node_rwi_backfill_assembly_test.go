package yagonode

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/rwibackfill"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type resumableDocumentScannerFixture struct{}

func (resumableDocumentScannerFixture) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, false, nil
}

func (resumableDocumentScannerFixture) Count(context.Context) (int, error) {
	return 0, nil
}

func (resumableDocumentScannerFixture) ScanStoredDocumentPagesFrom(
	context.Context,
	string,
	func(*documentstore.Document, string, bool) (bool, error),
) error {
	return nil
}

func TestOpenNodeRWIBackfillRejectsUnavailableScannerAndDependencies(t *testing.T) {
	if _, err := openNodeRWIBackfill(nil, nodeStorage{}); err == nil ||
		!strings.Contains(err.Error(), "resumable stored-document scan is unavailable") {
		t.Fatalf("missing scanner error = %v", err)
	}
	storage := nodeStorage{documentDirectory: resumableDocumentScannerFixture{}}
	if _, err := openNodeRWIBackfill(nil, storage); err == nil ||
		!strings.Contains(err.Error(), "open rwi backfill") {
		t.Fatalf("missing backfill dependency error = %v", err)
	}
}

func TestAssembleNodeReturnsRWIBackfillOpenError(t *testing.T) {
	restoreAssemblySeams(t)
	sentinel := errors.New("backfill unavailable")
	openRuntimeNodeRWIBackfill = func(*vault.Vault, nodeStorage) (*rwibackfill.Backfill, error) {
		return nil, sentinel
	}
	_, err := assembleNode(
		context.Background(),
		testConfig(t),
		openTestVault(t),
		http.DefaultClient,
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("assemble error = %v, want %v", err, sentinel)
	}
}
