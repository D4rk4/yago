package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type failingLineageRevisionDirectory struct {
	*lineageDocumentScript
	err error
}

func (f failingLineageRevisionDirectory) DocumentRevision(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, false, f.err
}

func TestContentClusterDocumentRevisionSurfacesRevisionReadFailure(t *testing.T) {
	wantErr := errors.New("revision failed")
	evictor := documentLineageEvictor{
		directory: failingLineageRevisionDirectory{
			lineageDocumentScript: &lineageDocumentScript{},
			err:                   wantErr,
		},
	}
	if _, _, err := evictor.contentClusterDocumentRevision(
		t.Context(),
		"https://document.example/",
	); !errors.Is(err, wantErr) {
		t.Fatalf("revision error = %v, want %v", err, wantErr)
	}
}
