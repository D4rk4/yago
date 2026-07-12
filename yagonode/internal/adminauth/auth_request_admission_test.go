package adminauth

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingAuthRequestBody struct {
	started  chan struct{}
	unblock  <-chan struct{}
	payload  []byte
	offset   int
	startOne sync.Once
}

func (b *blockingAuthRequestBody) Read(destination []byte) (int, error) {
	b.startOne.Do(func() { close(b.started) })
	<-b.unblock
	if b.offset == len(b.payload) {
		return 0, io.EOF
	}
	read := copy(destination, b.payload[b.offset:])
	b.offset += read

	return read, nil
}

func (*blockingAuthRequestBody) Close() error { return nil }

type authRequestAdmissionCase struct {
	name        string
	path        string
	contentType string
	payload     string
	handler     func(*testing.T, *Service) http.Handler
}

type blockedAuthRequests struct {
	completed <-chan *httptest.ResponseRecorder
	unblock   func()
}

type rejectedLoginBodyCase struct {
	name         string
	path         string
	contentType  string
	body         string
	firstStatus  int
	secondStatus int
	location     string
	handler      func(*testing.T, *Service) http.Handler
}

func jsonAuthSurface(t *testing.T, service *Service) http.Handler {
	return mountAuth(t, service)
}

var authRequestAdmissionCases = []authRequestAdmissionCase{
	{
		name:        "JSON login",
		path:        PathLogin,
		contentType: "application/json",
		payload:     "x",
		handler:     jsonAuthSurface,
	},
	{
		name:        "JSON setup",
		path:        PathSetup,
		contentType: "application/json",
		payload:     "x",
		handler:     jsonAuthSurface,
	},
	{
		name:        "HTML login",
		path:        PathLoginPage,
		contentType: formContentType,
		payload:     "%",
		handler:     htmlSurface,
	},
	{
		name:        "HTML setup",
		path:        PathSetupPage,
		contentType: formContentType,
		payload:     "%",
		handler:     htmlSurface,
	},
}

var rejectedLoginBodyCases = []rejectedLoginBodyCase{
	{
		name:         "malformed JSON",
		path:         PathLogin,
		contentType:  "application/json",
		body:         "x",
		firstStatus:  http.StatusBadRequest,
		secondStatus: http.StatusTooManyRequests,
		handler:      jsonAuthSurface,
	},
	{
		name:         "oversized JSON",
		path:         PathLogin,
		contentType:  "application/json",
		body:         oversizedCredentialsJSON(),
		firstStatus:  http.StatusRequestEntityTooLarge,
		secondStatus: http.StatusTooManyRequests,
		handler:      jsonAuthSurface,
	},
	{
		name:         "malformed form",
		path:         PathLoginPage,
		contentType:  formContentType,
		body:         "%",
		firstStatus:  http.StatusBadRequest,
		secondStatus: http.StatusSeeOther,
		location:     PathLoginPage + "?error=throttled",
		handler:      htmlSurface,
	},
	{
		name:         "oversized form",
		path:         PathLoginPage,
		contentType:  formContentType,
		body:         oversizedCredentialsForm(),
		firstStatus:  http.StatusRequestEntityTooLarge,
		secondStatus: http.StatusSeeOther,
		location:     PathLoginPage + "?error=throttled",
		handler:      htmlSurface,
	},
}

func authAdmissionRequest(
	t *testing.T,
	path, contentType, remoteAddress string,
	body io.ReadCloser,
) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil)
	request.Body = body
	request.ContentLength = -1
	request.RemoteAddr = remoteAddress
	request.Header.Set("Content-Type", contentType)

	return request
}

func waitForAuthBodyRead(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("auth request did not start reading its body")
	}
}

func beginBlockedAuthRequests(
	t *testing.T,
	test authRequestAdmissionCase,
	handler http.Handler,
) blockedAuthRequests {
	t.Helper()
	unblockChannel := make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(unblockChannel) }) }
	t.Cleanup(unblock)
	started := make([]chan struct{}, maximumConcurrentAuthRequests)
	completed := make(chan *httptest.ResponseRecorder, maximumConcurrentAuthRequests)
	for requestIndex := range maximumConcurrentAuthRequests {
		started[requestIndex] = make(chan struct{})
		body := &blockingAuthRequestBody{
			started: started[requestIndex],
			unblock: unblockChannel,
			payload: []byte(test.payload),
		}
		request := authAdmissionRequest(
			t,
			test.path,
			test.contentType,
			fmt.Sprintf("192.0.2.%d:1234", requestIndex+1),
			body,
		)
		go func() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			completed <- recorder
		}()
	}
	for _, bodyStarted := range started {
		waitForAuthBodyRead(t, bodyStarted)
	}

	return blockedAuthRequests{completed: completed, unblock: unblock}
}

func assertAuthAdmissionOverflow(
	t *testing.T,
	test authRequestAdmissionCase,
	handler http.Handler,
) {
	t.Helper()
	probe := &authBodyProbe{}
	overflow := serveAuthBody(handler, test.path, test.contentType, probe, -1)
	if overflow.Code != http.StatusServiceUnavailable ||
		overflow.Header().Get("Retry-After") != "1" || probe.reads.Load() != 0 {
		t.Fatalf(
			"overflow response = %d retry=%q reads=%d",
			overflow.Code,
			overflow.Header().Get("Retry-After"),
			probe.reads.Load(),
		)
	}
}

func assertAuthAdmissionCase(t *testing.T, test authRequestAdmissionCase) {
	t.Helper()
	handler := test.handler(t, testService(t))
	blocked := beginBlockedAuthRequests(t, test, handler)
	assertAuthAdmissionOverflow(t, test, handler)
	blocked.unblock()
	for range maximumConcurrentAuthRequests {
		if recorder := <-blocked.completed; recorder.Code != http.StatusBadRequest {
			t.Fatalf("blocked request status = %d", recorder.Code)
		}
	}
	recovery := serveAuthBody(
		handler,
		test.path,
		test.contentType,
		io.NopCloser(strings.NewReader(test.payload)),
		-1,
	)
	if recovery.Code != http.StatusBadRequest {
		t.Fatalf("recovery status = %d", recovery.Code)
	}
}

func TestAuthRequestAdmissionBoundsSlowBodiesAndRecovers(t *testing.T) {
	for _, test := range authRequestAdmissionCases {
		t.Run(test.name, func(t *testing.T) { assertAuthAdmissionCase(t, test) })
	}
}

func assertRejectedLoginBody(t *testing.T, test rejectedLoginBodyCase) {
	t.Helper()
	observer := &countingObserver{}
	handler := test.handler(t, observerService(t, observer))
	first := serveAuthBody(
		handler,
		test.path,
		test.contentType,
		io.NopCloser(strings.NewReader(test.body)),
		-1,
	)
	if first.Code != test.firstStatus {
		t.Fatalf("first status = %d", first.Code)
	}
	assertAuthBodyUnread(
		t,
		handler,
		test.path,
		test.secondStatus,
		test.location,
	)
	if observer.loginFailure != 1 || observer.loginThrottled != 1 {
		t.Fatalf(
			"observer failure/throttled = %d/%d",
			observer.loginFailure,
			observer.loginThrottled,
		)
	}
}

func TestRejectedLoginBodiesConsumeFailureBudget(t *testing.T) {
	for _, test := range rejectedLoginBodyCases {
		t.Run(test.name, func(t *testing.T) { assertRejectedLoginBody(t, test) })
	}
}
