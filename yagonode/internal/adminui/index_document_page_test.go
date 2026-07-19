package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeDocumentDetailSource struct {
	detail DocumentDetail
	found  bool
	err    error
	key    string
}

func (f *fakeDocumentDetailSource) DocumentDetail(
	_ context.Context,
	key string,
) (DocumentDetail, bool, error) {
	f.key = key

	return f.detail, f.found, f.err
}

func TestConsoleIndexLinksToBoundedDocumentInspection(t *testing.T) {
	t.Parallel()

	documents := &fakeDocuments{page: DocumentPage{
		Documents: []DocumentSummary{{
			Key: "https://example.test/a?x=1&y=2", URL: "https://example.test/a?x=1&y=2",
		}},
		Matched: 1,
	}}
	body := do(t, New(Options{
		Index:          fakeIndex{snap: IndexStats{Available: true}},
		Documents:      documents,
		DocumentDetail: &fakeDocumentDetailSource{},
	}), indexPath).body
	if !strings.Contains(body, ">Inspect</a>") ||
		!strings.Contains(body, "url=https%3A%2F%2Fexample.test%2Fa%3Fx%3D1%26y%3D2") {
		t.Fatalf("document inspection link = %s", body)
	}
}

func TestConsoleDocumentInspectionEscapesStoredContent(t *testing.T) {
	t.Parallel()

	key := "https://example.test/document"
	source := &fakeDocumentDetailSource{
		found: true,
		detail: DocumentDetail{
			URL:                     key,
			Title:                   `<script>alert("title")</script>`,
			ContentPreview:          `<img src=x onerror="alert(1)">`,
			ContentBytes:            999,
			ContentPreviewTruncated: true,
			Safety:                  DocumentSafetyDetail{Rating: "unknown"},
			Metadata: []DocumentMetadataDetail{{
				Name: "description", Value: `<svg onload="alert(2)">`,
			}},
			MetadataTotal: 1,
		},
	}
	body := do(t, New(Options{DocumentDetail: source}),
		indexDocumentPath+"?url="+url.QueryEscape(key)).body
	if source.key != key {
		t.Fatalf("lookup key = %q", source.key)
	}
	for _, want := range []string{
		"Document detail", "Stored content preview", "999 stored byte(s)",
		"The preview is truncated", "&lt;script&gt;", "&lt;img", "&lt;svg",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("document detail missing %q in %s", want, body)
		}
	}
	for _, forbidden := range []string{"<script>", "<img src=x", "<svg onload"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("unescaped stored content %q in %s", forbidden, body)
		}
	}
}

func TestConsoleDocumentInspectionMissingAndFailureStates(t *testing.T) {
	t.Parallel()

	if got := do(t, New(Options{}), indexDocumentPath+"?url=x"); got.status != http.StatusNotFound {
		t.Fatalf("missing source status = %d", got.status)
	}
	source := &fakeDocumentDetailSource{}
	if got := do(
		t,
		New(Options{DocumentDetail: source}),
		indexDocumentPath,
	); got.status != http.StatusBadRequest {
		t.Fatalf("missing URL status = %d", got.status)
	}
	if got := do(
		t,
		New(Options{DocumentDetail: source}),
		indexDocumentPath+"?url=x",
	); got.status != http.StatusNotFound {
		t.Fatalf("missing document status = %d", got.status)
	}
	source.err = errors.New("private storage detail")
	failed := do(t, New(Options{DocumentDetail: source}), indexDocumentPath+"?url=x")
	if failed.status != http.StatusOK ||
		!strings.Contains(failed.body, "Document detail is not available.") ||
		strings.Contains(failed.body, "private storage detail") {
		t.Fatalf("lookup failure = status %d body %s", failed.status, failed.body)
	}
}
