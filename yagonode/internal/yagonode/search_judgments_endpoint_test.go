package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/judgments"
)

type stubCurator struct {
	list      []judgments.Judgment
	listErr   error
	putErr    error
	deleted   bool
	deleteErr error
	putCalls  int
}

func (s *stubCurator) List(context.Context) ([]judgments.Judgment, error) {
	return s.list, s.listErr
}

func (s *stubCurator) Put(_ context.Context, judgment judgments.Judgment) error {
	s.putCalls++
	if s.putErr != nil {
		return s.putErr
	}
	s.list = append(s.list, judgment)

	return nil
}

func (s *stubCurator) Delete(context.Context, string) (bool, error) {
	return s.deleted, s.deleteErr
}

func serveJudgments(
	t *testing.T,
	store judgmentCurator,
	method, target, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	newSearchJudgmentsEndpoint(store).ServeHTTP(rec, req)

	return rec
}

func TestJudgmentsEndpointUnavailable(t *testing.T) {
	rec := serveJudgments(t, nil, http.MethodGet, pathSearchJudgments, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestJudgmentsEndpointRejectsUnknownMethod(t *testing.T) {
	rec := serveJudgments(t, &stubCurator{}, http.MethodPut, pathSearchJudgments, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestJudgmentsEndpointLists(t *testing.T) {
	store := &stubCurator{list: []judgments.Judgment{{
		Query:  "linux",
		Grades: map[string]int{"https://a.example/": 2},
	}}}
	rec := serveJudgments(t, store, http.MethodGet, pathSearchJudgments, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "linux") {
		t.Fatalf("body %q missing the judgment", rec.Body.String())
	}
}

func TestJudgmentsEndpointListError(t *testing.T) {
	rec := serveJudgments(
		t,
		&stubCurator{listErr: errors.New("boom")},
		http.MethodGet,
		pathSearchJudgments,
		"",
	)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestJudgmentsEndpointPutStoresAndLists(t *testing.T) {
	store := &stubCurator{}
	body := `{"query":"linux","grades":{"https://a.example/":2}}`
	rec := serveJudgments(t, store, http.MethodPost, pathSearchJudgments, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if store.putCalls != 1 {
		t.Fatalf("Put calls = %d, want 1", store.putCalls)
	}
	if !strings.Contains(rec.Body.String(), "linux") {
		t.Fatalf("response %q must echo the stored judgments", rec.Body.String())
	}
}

func TestJudgmentsEndpointPutDecodeError(t *testing.T) {
	rec := serveJudgments(t, &stubCurator{}, http.MethodPost, pathSearchJudgments, "{not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestJudgmentsEndpointPutStoreError(t *testing.T) {
	store := &stubCurator{putErr: errors.New("invalid")}
	body := `{"query":"linux","grades":{"https://a.example/":2}}`
	rec := serveJudgments(t, store, http.MethodPost, pathSearchJudgments, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestJudgmentsEndpointDeleteRemoves(t *testing.T) {
	rec := serveJudgments(
		t, &stubCurator{deleted: true}, http.MethodDelete, pathSearchJudgments+"?query=linux", "",
	)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestJudgmentsEndpointDeleteNotFound(t *testing.T) {
	rec := serveJudgments(
		t, &stubCurator{deleted: false}, http.MethodDelete, pathSearchJudgments+"?query=x", "",
	)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestJudgmentsEndpointDeleteError(t *testing.T) {
	rec := serveJudgments(
		t,
		&stubCurator{deleteErr: errors.New("boom")},
		http.MethodDelete,
		pathSearchJudgments+"?query=x",
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
