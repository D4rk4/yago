package publicportal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPortalLinksAgentAPIGuide(t *testing.T) {
	status, body := get(t, New(&fakeSource{}, false), "https://node.example/")
	if status != http.StatusOK {
		t.Fatalf("portal status = %d", status)
	}
	if !strings.Contains(body, `<a href="/api">Use the agent API</a>`) {
		t.Fatal("portal does not link to the agent API guide")
	}
}

func TestAgentAPIGuideShowsAuthenticationAndRequestExample(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api", nil)
	AgentAPIGuide().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != htmlType {
		t.Fatalf("content type = %q", contentType)
	}
	for _, expected := range []string{
		"Create an API key",
		"search:read",
		"search:raw",
		"Authorization: Bearer",
		`POST /search`,
		`POST /extract`,
		`POST /crawl`,
		`POST /map`,
	} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Errorf("guide does not contain %q", expected)
		}
	}
}

func TestAgentAPIGuideRejectsOtherMethodsAndPaths(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodPost, path: "/api", status: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/api/extra", status: http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(test.method, test.path, nil)
		AgentAPIGuide().ServeHTTP(recorder, request)
		if recorder.Code != test.status {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, recorder.Code, test.status)
		}
	}
}
