package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type contentClusterBatchFailure struct {
	replacements []contentcluster.EvidenceReplacement
	err          error
}

func (s contentClusterBatchFailure) ReplaceBatch(
	context.Context,
	[]contentcluster.Evidence,
) ([]contentcluster.EvidenceReplacement, error) {
	return s.replacements, s.err
}

func TestReplaceDocumentClusterBatchReportsProviderFailure(t *testing.T) {
	consumer := &IngestConsumer{}
	_, err := consumer.replaceDocumentClusterBatch(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: "https://example.org/"}},
		contentClusterBatchFailure{err: errors.New("cluster unavailable")},
	)
	if err == nil {
		t.Fatal("content cluster provider failure was not reported")
	}
}

func TestReplaceDocumentClusterBatchRejectsIncompleteResults(t *testing.T) {
	consumer := &IngestConsumer{}
	_, err := consumer.replaceDocumentClusterBatch(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: "https://example.org/"}},
		contentClusterBatchFailure{},
	)
	if err == nil {
		t.Fatal("incomplete content cluster results were accepted")
	}
}
