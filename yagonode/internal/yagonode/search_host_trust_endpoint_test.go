package yagonode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

type failingTrustedDomainCatalog struct {
	policy hosttrust.Policy
	err    error
}

func (catalog *failingTrustedDomainCatalog) Current() hosttrust.Policy {
	return catalog.policy
}

func (catalog *failingTrustedDomainCatalog) Replace(
	context.Context,
	hosttrust.Policy,
) error {
	return catalog.err
}

func TestSearchHostTrustEndpointReadsAndReplacesPolicy(t *testing.T) {
	catalog := testHostTrustCatalog(t)
	endpoint := newSearchHostTrustEndpoint(catalog)

	get := serveHostTrust(endpoint, http.MethodGet, "")
	if get.Code != http.StatusOK || get.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET status=%d content-type=%q", get.Code, get.Header().Get("Content-Type"))
	}
	assertHostTrustResponse(t, get.Body.String(), hosttrust.Policy{Domains: []string{}})

	put := serveHostTrust(
		endpoint,
		http.MethodPut,
		`{"blend":0.4,"domains":["HTTPS://www.Example.com/path","b.example"]}`,
	)
	want := hosttrust.Policy{Blend: 0.4, Domains: []string{"b.example", "example.com"}}
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}
	assertHostTrustResponse(t, put.Body.String(), want)
	if got := catalog.Current(); !reflect.DeepEqual(got, want) {
		t.Fatalf("stored policy = %#v, want %#v", got, want)
	}
}

func TestSearchHostTrustEndpointRejectsUnavailableMethodAndStorageFailure(t *testing.T) {
	unavailable := serveHostTrust(newSearchHostTrustEndpoint(nil), http.MethodGet, "")
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d", unavailable.Code)
	}

	catalog := &failingTrustedDomainCatalog{err: errors.New("disk")}
	method := serveHostTrust(
		searchHostTrustEndpoint{catalog: catalog},
		http.MethodPost,
		"",
	)
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != "GET, PUT" {
		t.Fatalf("method status=%d allow=%q", method.Code, method.Header().Get("Allow"))
	}

	failed := serveHostTrust(
		searchHostTrustEndpoint{catalog: catalog},
		http.MethodPut,
		`{"blend":0.2,"domains":["example.com"]}`,
	)
	if failed.Code != http.StatusBadRequest ||
		!strings.Contains(failed.Body.String(), "apply host trust policy: disk") {
		t.Fatalf("failed replace status=%d body=%s", failed.Code, failed.Body.String())
	}
}

func TestSearchHostTrustEndpointRejectsInvalidBodies(t *testing.T) {
	endpoint := searchHostTrustEndpoint{catalog: &failingTrustedDomainCatalog{}}
	cases := []struct {
		name string
		body string
	}{
		{name: "malformed", body: "{"},
		{name: "unknown", body: `{"blend":0,"domains":[],"future":true}`},
		{name: "trailing", body: `{"blend":0,"domains":[]} {}`},
		{name: "oversized", body: `{"blend":0,"domains":["` +
			strings.Repeat("a", maximumHostTrustRequestBody) + `"]}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := serveHostTrust(endpoint, http.MethodPut, test.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func testHostTrustCatalog(t *testing.T) *hosttrust.Catalog {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	catalog, err := hosttrust.Open(t.Context(), storage)
	if err != nil {
		t.Fatal(err)
	}

	return catalog
}

func serveHostTrust(endpoint http.Handler, method, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(
		context.Background(),
		method,
		pathSearchHostTrust,
		strings.NewReader(body),
	)
	recorder := httptest.NewRecorder()
	endpoint.ServeHTTP(recorder, request)

	return recorder
}

func assertHostTrustResponse(t *testing.T, body string, want hosttrust.Policy) {
	t.Helper()
	var response searchHostTrustResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Policy, want) {
		t.Fatalf("response policy = %#v, want %#v", response.Policy, want)
	}
}
