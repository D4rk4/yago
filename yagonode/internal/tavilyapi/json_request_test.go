package tavilyapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type unreadJSONBody struct {
	read bool
}

func (b *unreadJSONBody) Read([]byte) (int, error) {
	b.read = true

	return 0, io.EOF
}

func (*unreadJSONBody) Close() error { return nil }

func TestSearchJSONRequestBodyBoundary(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		body := searchRequestBody(int(maximumJSONRequestBodyBytes))
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			PathSearch,
			strings.NewReader(body),
		)
		request.Header.Set("Authorization", "Bearer "+searchTestKey)

		newTestSearchEndpoint(&fakeSearcher{}, nil).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("chunked overflow", func(t *testing.T) {
		body := searchRequestBody(int(maximumJSONRequestBodyBytes) + 1)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			PathSearch,
			strings.NewReader(body),
		)
		request.ContentLength = -1
		request.TransferEncoding = []string{"chunked"}
		request.Header.Set("Authorization", "Bearer "+searchTestKey)
		request.Header.Set(requestIDHeader, "chunked-overflow")

		newTestSearchEndpoint(&fakeSearcher{}, nil).ServeHTTP(recorder, request)

		assertRequestTooLarge(t, recorder, "chunked-overflow")
	})
}

func TestSearchRejectsUnauthenticatedBodyBeforeRead(t *testing.T) {
	for _, authorization := range []string{"", "Bearer wrong"} {
		body := &unreadJSONBody{}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			PathSearch,
			nil,
		)
		request.Body = body
		request.ContentLength = maximumJSONRequestBodyBytes + 1
		request.Header.Set("Authorization", authorization)

		newTestSearchEndpoint(&fakeSearcher{}, nil).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("authorization %q status = %d", authorization, recorder.Code)
		}
		if body.read {
			t.Fatalf("authorization %q read the request body", authorization)
		}
	}
}

func TestExtractRejectsUnauthenticatedBodyBeforeRead(t *testing.T) {
	body := &unreadJSONBody{}
	authorizer := &stubScopeAuthorizer{decision: DecisionAllow}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathExtract,
		nil,
	)
	request.Body = body
	request.ContentLength = maximumJSONRequestBodyBytes + 1

	NewExtractEndpointWithAccess(
		nil,
		SearchAccessPolicy{Authorizer: authorizer},
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
	if body.read || authorizer.calls != 0 {
		t.Fatalf("body read = %t, authorizer calls = %d", body.read, authorizer.calls)
	}
}

func TestSiblingJSONEndpointsRejectOversizedBodies(t *testing.T) {
	body := strings.Repeat("x", int(maximumJSONRequestBodyBytes)+1)
	tests := []struct {
		name    string
		path    string
		handler http.Handler
		key     string
	}{
		{
			name: "extract",
			path: PathExtract,
			handler: NewExtractEndpointWithAccess(
				nil,
				SearchAccessPolicy{BearerToken: extractTestKey},
			),
			key: extractTestKey,
		},
		{
			name: "crawl",
			path: PathCrawl,
			handler: crawlEndpoint{
				access:  SearchAccessPolicy{BearerToken: crawlTestKey},
				fetcher: testSite(),
			},
			key: crawlTestKey,
		},
		{
			name: "map",
			path: PathMap,
			handler: crawlEndpoint{
				access:  SearchAccessPolicy{BearerToken: crawlTestKey},
				fetcher: testSite(),
				mapOnly: true,
			},
			key: crawlTestKey,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				test.path,
				strings.NewReader(body),
			)
			request.Header.Set("Authorization", "Bearer "+test.key)
			request.Header.Set(requestIDHeader, test.name)

			test.handler.ServeHTTP(recorder, request)

			assertRequestTooLarge(t, recorder, test.name)
		})
	}
}

func searchRequestBody(size int) string {
	prefix := `{"query":"`
	suffix := `"}`

	return prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
}

func assertRequestTooLarge(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	requestID string,
) {
	t.Helper()
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error.Code != requestTooLargeErrorCode ||
		envelope.Error.Message != requestTooLargeErrorMessage ||
		envelope.Detail.Error != requestTooLargeErrorMessage ||
		envelope.RequestID != requestID {
		t.Fatalf("response = %#v", envelope)
	}
}
