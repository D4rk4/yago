package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestQueryHandlerParsesMultipartForm(t *testing.T) {
	h := newTestHarness(t)
	h.counter.count = 7

	req := yacyproto.QueryRequest{YouAre: h.ident.Hash, Object: yacyproto.ObjectRWICount}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, values := range req.Form() {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatalf("write field %s: %v", key, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, yacyproto.PathQuery, &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	h.mux().ServeHTTP(rec, r)

	resp, err := yacyproto.ParseQueryResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Response != 7 {
		t.Errorf("Response = %d, want 7", resp.Response)
	}
}
