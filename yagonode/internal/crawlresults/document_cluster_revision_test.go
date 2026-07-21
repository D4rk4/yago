package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type failingDocumentRevisionReceiver struct {
	err error
}

func (f failingDocumentRevisionReceiver) Receive(
	context.Context,
	[]documentstore.Document,
) (documentstore.Receipt, error) {
	return documentstore.Receipt{}, nil
}

func (f failingDocumentRevisionReceiver) DocumentRevision(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, false, f.err
}

func TestDocumentClusterRevisionSurfacesRevisionReadFailure(t *testing.T) {
	wantErr := errors.New("revision failed")
	consumer := &IngestConsumer{
		documents: failingDocumentRevisionReceiver{err: wantErr},
	}
	if _, _, err := consumer.documentClusterRevision(
		t.Context(),
		nil,
		"https://document.example/",
	); !errors.Is(err, wantErr) {
		t.Fatalf("revision error = %v, want %v", err, wantErr)
	}
}
