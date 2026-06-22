package httpguard_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestFailureResponsesSetStatus(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func(rec *httptest.ResponseRecorder)
		want int
	}{
		{"bad request", func(rec *httptest.ResponseRecorder) {
			httpguard.FailBadRequest(ctx, rec, errors.New("boom"))
		}, http.StatusBadRequest},
		{"too large", func(rec *httptest.ResponseRecorder) {
			httpguard.FailRequestTooLarge(ctx, rec, errors.New("boom"))
		}, http.StatusRequestEntityTooLarge},
		{"method", func(rec *httptest.ResponseRecorder) {
			httpguard.FailMethodNotAllowed(ctx, rec, http.MethodPut)
		}, http.StatusMethodNotAllowed},
		{"internal", func(rec *httptest.ResponseRecorder) {
			httpguard.FailInternal(ctx, rec, "op", errors.New("boom"))
		}, http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.fn(rec)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestWriteWireMessageEncodesBody(t *testing.T) {
	rec := httptest.NewRecorder()
	httpguard.WriteWireMessage(context.Background(), rec, yacymodel.Message{"k": "v"})

	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=UTF-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("body is empty")
	}
}

func TestParseDecodesGzipBody(t *testing.T) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write([]byte("a=b")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferURL,
		&buf,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Content-Encoding", "gzip")

	form, _, cancel, ok := testGuard().Parse(rec, req, yacyproto.TransferURLEndpointMethods)
	if !ok {
		t.Fatalf("Parse rejected a gzip body, status %d", rec.Code)
	}
	defer cancel()
	if form.Get("a") != "b" {
		t.Fatalf("form[a] = %q, want b", form.Get("a"))
	}
}
