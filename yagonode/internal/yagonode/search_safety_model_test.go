package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/safetymodel"
)

type safetyModelCatalogFixture struct {
	status      safetymodel.Status
	snapshot    []byte
	activated   safetymodel.Snapshot
	activateErr error
	rolledBack  bool
	rollbackErr error
}

func (fixture *safetyModelCatalogFixture) Status() safetymodel.Status {
	return fixture.status
}

func (fixture *safetyModelCatalogFixture) ActiveSnapshotJSON() []byte {
	return fixture.snapshot
}

func (fixture *safetyModelCatalogFixture) Activate(
	_ context.Context,
	snapshot safetymodel.Snapshot,
) error {
	fixture.activated = snapshot
	if fixture.activateErr == nil {
		fixture.status = safetymodel.Status{Active: true, Revision: snapshot.Revision}
	}

	return fixture.activateErr
}

func (fixture *safetyModelCatalogFixture) Rollback(context.Context) (bool, error) {
	return fixture.rolledBack, fixture.rollbackErr
}

func TestSearchSafetyModelEndpoint(t *testing.T) {
	fixture := &safetyModelCatalogFixture{
		status:   safetymodel.Status{Active: true, Revision: "v1"},
		snapshot: []byte(`{"revision":"v1"}`),
	}
	recorder := httptest.NewRecorder()
	newSearchSafetyModelEndpoint(fixture).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchSafetyModel, nil),
	)
	if recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), `"revision":"v1"`) ||
		recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	method := httptest.NewRecorder()
	newSearchSafetyModelEndpoint(fixture).ServeHTTP(
		method,
		httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathSearchSafetyModel, nil),
	)
	if method.Code != http.StatusMethodNotAllowed ||
		method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method response = %d %q", method.Code, method.Header().Get("Allow"))
	}
	unavailable := httptest.NewRecorder()
	newSearchSafetyModelEndpoint(nil).ServeHTTP(
		unavailable,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSearchSafetyModel, nil),
	)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable = %d", unavailable.Code)
	}
}

func TestSearchSafetyTrainEndpointActivatesModel(t *testing.T) {
	fixture := &safetyModelCatalogFixture{}
	recorder := postSafetyTraining(t, fixture, safetyTrainingJSON("safety-v1"))
	if recorder.Code != http.StatusOK || fixture.activated.Revision != "safety-v1" ||
		!strings.Contains(recorder.Body.String(), `"revision":"safety-v1"`) {
		t.Fatalf(
			"response = %d %q, activation = %#v",
			recorder.Code,
			recorder.Body.String(),
			fixture.activated,
		)
	}
}

func TestSearchSafetyTrainEndpointRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		catalog safetyModelCatalog
		method  string
		body    string
		want    int
	}{
		{"method", &safetyModelCatalogFixture{}, http.MethodGet, `{}`, http.StatusMethodNotAllowed},
		{"unavailable", nil, http.MethodPost, `{}`, http.StatusServiceUnavailable},
		{"bad json", &safetyModelCatalogFixture{}, http.MethodPost, `{`, http.StatusBadRequest},
		{
			"unknown field",
			&safetyModelCatalogFixture{},
			http.MethodPost,
			`{"extra":1}`,
			http.StatusBadRequest,
		},
		{"trailing", &safetyModelCatalogFixture{}, http.MethodPost, `{} {}`, http.StatusBadRequest},
		{
			"malformed trailing",
			&safetyModelCatalogFixture{},
			http.MethodPost,
			`{} {`,
			http.StatusBadRequest,
		},
		{
			"invalid rating",
			&safetyModelCatalogFixture{},
			http.MethodPost,
			`{"documents":[{"text":"x","rating":"other"}]}`,
			http.StatusBadRequest,
		},
		{
			"invalid corpus",
			&safetyModelCatalogFixture{},
			http.MethodPost,
			`{"revision":"v1","documents":[]}`,
			http.StatusBadRequest,
		},
		{
			"bad revision",
			&safetyModelCatalogFixture{},
			http.MethodPost,
			safetyTrainingJSON("bad/value"),
			http.StatusBadRequest,
		},
		{
			"activation",
			&safetyModelCatalogFixture{activateErr: errors.New("disk")},
			http.MethodPost,
			safetyTrainingJSON("v1"),
			http.StatusInternalServerError,
		},
		{
			"too large",
			&safetyModelCatalogFixture{},
			http.MethodPost,
			strings.Repeat(" ", maximumSafetyTrainingBody+1),
			http.StatusBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(
				t.Context(), test.method, pathSearchSafetyTrain, strings.NewReader(test.body),
			)
			newSearchSafetyTrainEndpoint(test.catalog).ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
			}
			if test.name == "method" && recorder.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("Allow = %q", recorder.Header().Get("Allow"))
			}
		})
	}
}

func TestSearchSafetyRollbackEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		catalog safetyModelCatalog
		method  string
		want    int
	}{
		{"method", &safetyModelCatalogFixture{}, http.MethodGet, http.StatusMethodNotAllowed},
		{"unavailable", nil, http.MethodPost, http.StatusServiceUnavailable},
		{
			"failure",
			&safetyModelCatalogFixture{rollbackErr: errors.New("disk")},
			http.MethodPost,
			http.StatusInternalServerError,
		},
		{"empty", &safetyModelCatalogFixture{}, http.MethodPost, http.StatusConflict},
		{"success", &safetyModelCatalogFixture{rolledBack: true}, http.MethodPost, http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			newSearchSafetyRollbackEndpoint(test.catalog).ServeHTTP(
				recorder,
				httptest.NewRequestWithContext(
					t.Context(),
					test.method,
					pathSearchSafetyRollback,
					nil,
				),
			)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
			}
			if test.name == "method" && recorder.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("Allow = %q", recorder.Header().Get("Allow"))
			}
		})
	}
}

func TestSafetyTrainingDocuments(t *testing.T) {
	documents, err := safetyTrainingDocuments([]safetyTrainingDocument{
		{Text: "general", Rating: " General "},
		{Text: "explicit", Rating: "EXPLICIT"},
	})
	if err != nil || len(documents) != 2 || documents[0].Rating == documents[1].Rating {
		t.Fatalf("documents = %#v, %v", documents, err)
	}
}

func postSafetyTraining(
	t *testing.T,
	catalog safetyModelCatalog,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	newSearchSafetyTrainEndpoint(catalog).ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			pathSearchSafetyTrain,
			strings.NewReader(body),
		),
	)

	return recorder
}

func safetyTrainingJSON(revision string) string {
	return `{"revision":"` + revision + `","documents":[` +
		`{"text":"family archive catalogue calm public alpha","rating":"general"},` +
		`{"text":"family guide library calm public beta","rating":"general"},` +
		`{"text":"family reference collection calm public gamma","rating":"general"},` +
		`{"text":"restricted mature section private lambda","rating":"explicit"},` +
		`{"text":"restricted private catalogue mature omega","rating":"explicit"},` +
		`{"text":"restricted archive private mature sigma","rating":"explicit"}` +
		`]}`
}
