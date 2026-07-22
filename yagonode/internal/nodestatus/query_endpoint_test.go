package nodestatus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type mountedQueryStatus struct{}

func (mountedQueryStatus) Version(context.Context) string {
	return "test-version"
}

func (mountedQueryStatus) Uptime(context.Context) int {
	return 12
}

func newQueryEndpoint(counts stubCounter) queryEndpoint {
	return queryEndpoint{
		identity: nodeidentity.Identity{
			Hash:        yagomodel.WordHash("self"),
			NetworkName: "freeworld",
		},
		rwi: counts,
	}
}

func serveQuery(
	t *testing.T,
	e queryEndpoint,
	req yagoproto.QueryRequest,
) yagoproto.QueryResponse {
	t.Helper()

	resp, err := e.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	return resp
}

func queryRequest(object yagoproto.QueryObject) yagoproto.QueryRequest {
	return yagoproto.QueryRequest{
		NetworkName: "freeworld",
		YouAre:      yagomodel.WordHash("self").String(),
		Iam:         yagomodel.WordHash("caller").String(),
		Object:      object,
	}
}

func TestQueryAnswersSupportedObjects(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11, rwiURLs: 4})
	word := yagomodel.WordHash("term")

	cases := []struct {
		object yagoproto.QueryObject
		env    string
		want   int
	}{
		{object: yagoproto.ObjectRWICount, want: 11},
		{object: yagoproto.ObjectRWIURLCount, env: word.String(), want: 4},
		{object: yagoproto.ObjectLURLCount, want: 1},
	}
	for _, c := range cases {
		req := queryRequest(c.object)
		req.Env = c.env
		resp := serveQuery(t, endpoint, req)
		if resp.Response != c.want {
			t.Fatalf("%s: Response = %d, want %d", c.object, resp.Response, c.want)
		}
	}
}

func TestQueryAnswersWantedObjectsWithZero(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	for _, object := range []yagoproto.QueryObject{
		yagoproto.ObjectWantedLURLs,
		yagoproto.ObjectWantedPURLs,
		yagoproto.ObjectWantedWord,
		yagoproto.ObjectWantedRWI,
		yagoproto.ObjectWantedSeeds,
	} {
		resp := serveQuery(t, endpoint, queryRequest(object))
		if resp.Response != 0 {
			t.Fatalf("%s: Response = %d, want 0", object, resp.Response)
		}
	}
}

func TestQueryAnswersMalformedRWIURLCountEnvWithZero(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwiURLs: 4})

	resp := serveQuery(t, endpoint, queryRequest(yagoproto.ObjectRWIURLCount))
	if resp.Response != 0 {
		t.Fatalf("Response = %d, want 0", resp.Response)
	}
}

func TestQueryRejectsUnknownObject(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})
	endpoint.identity.Start = time.UnixMilli(123456789)
	endpoint.now = func() time.Time {
		return time.Date(2026, 7, 21, 12, 34, 56, 0, time.UTC)
	}

	resp := serveQuery(t, endpoint, queryRequest(yagoproto.QueryObject("unknown")))
	if !resp.UnresolvedResponse || resp.Magic != "123456789" || resp.MyTime != "20260721123456" {
		t.Fatalf("Response = %#v", resp)
	}
}

func TestQueryIncludesMagicBeforeAuthenticationAndTimeAfterAuthentication(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})
	endpoint.identity.Start = time.UnixMilli(987654321)
	endpoint.now = func() time.Time {
		return time.Date(2026, 7, 21, 12, 34, 56, 0, time.UTC)
	}

	wrongNetwork := queryRequest(yagoproto.ObjectRWICount)
	wrongNetwork.NetworkName = "other"
	rejected := serveQuery(t, endpoint, wrongNetwork)
	if rejected.Magic != "987654321" || rejected.MyTime != "" ||
		rejected.Response != yagoproto.QueryResponseRejected {
		t.Fatalf("unauthenticated response = %#v", rejected)
	}

	wrongTarget := queryRequest(yagoproto.ObjectRWICount)
	wrongTarget.YouAre = yagomodel.WordHash("other").String()
	authenticated := serveQuery(t, endpoint, wrongTarget)
	if authenticated.Magic != "987654321" || authenticated.MyTime != "20260721123456" ||
		authenticated.Response != yagoproto.QueryResponseRejected {
		t.Fatalf("authenticated wrong-target response = %#v", authenticated)
	}
}

func TestQueryAuthenticatesArbitraryIamBytes(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})
	endpoint.identity.AuthenticationMode = yagoproto.NetworkAuthenticationSaltedMagic
	endpoint.identity.AuthenticationEssentials = "shared-secret"
	req := queryRequest(yagoproto.ObjectRWICount)
	req.Iam = "caller identity outside the hash vocabulary"
	req.Key = "salt1234"
	req.MagicMD5 = yagoproto.MagicMD5(req.Key, req.Iam, endpoint.identity.AuthenticationEssentials)

	response := serveQuery(t, endpoint, req)
	if response.Response != 11 {
		t.Fatalf("Response = %d, want 11", response.Response)
	}
}

func TestQueryRejectsWrongTarget(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	req := queryRequest(yagoproto.ObjectRWICount)
	req.YouAre = yagomodel.WordHash("other").String()
	resp := serveQuery(t, endpoint, req)

	if resp.Response != yagoproto.QueryResponseRejected {
		t.Fatalf("Response = %d, want rejected for wrong target", resp.Response)
	}
}

func TestQueryFailsOnCountError(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{err: errors.New("boom")})

	if _, err := endpoint.Serve(
		context.Background(),
		queryRequest(yagoproto.ObjectRWICount),
	); err == nil {
		t.Fatal("Serve returned nil error, want count failure")
	}
}

func TestQueryRWIURLCountFailureUsesUnresolvedTemplate(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{err: errors.New("boom")})
	request := queryRequest(yagoproto.ObjectRWIURLCount)
	request.Env = yagomodel.WordHash("term").String()

	response := serveQuery(t, endpoint, request)
	if !response.UnresolvedResponse || response.Response != yagoproto.QueryResponseRejected {
		t.Fatalf("Response = %#v", response)
	}
}

func TestMountQueryServesWireRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedQueryStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	counts := stubCounter{rwi: 11}
	identity := nodeidentity.Identity{
		Hash:        yagomodel.WordHash("self"),
		NetworkName: "freeworld",
	}
	MountQuery(router, identity, counts)
	form := queryRequest(yagoproto.ObjectRWICount).Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yagoproto.PathQuery+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != queryContentType {
		t.Fatalf("Content-Type = %q, want %q", contentType, queryContentType)
	}
	msg, err := yagomodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	resp, err := yagoproto.ParseQueryResponse(msg)
	if err != nil {
		t.Fatalf("parse query response: %v", err)
	}
	if resp.Response != 11 {
		t.Fatalf("Response = %d, want 11", resp.Response)
	}
	if resp.Version != "test-version" || resp.Uptime != 12 {
		t.Fatalf("header = version %q uptime %d", resp.Version, resp.Uptime)
	}
}

func TestMountQueryDefaultsMissingNetworkName(t *testing.T) {
	for _, mode := range []yagoproto.NetworkAuthenticationMode{
		yagoproto.NetworkAuthenticationUncontrolled,
		yagoproto.NetworkAuthenticationSaltedMagic,
	} {
		t.Run(string(mode), func(t *testing.T) {
			response := mountedQueryNetworkResponse(t, mode, false)
			if response.Response != 11 {
				t.Fatalf("Response = %d, want 11", response.Response)
			}
		})
	}
}

func TestMountQueryRejectsExplicitlyEmptyNetworkName(t *testing.T) {
	for _, mode := range []yagoproto.NetworkAuthenticationMode{
		yagoproto.NetworkAuthenticationUncontrolled,
		yagoproto.NetworkAuthenticationSaltedMagic,
	} {
		t.Run(string(mode), func(t *testing.T) {
			response := mountedQueryNetworkResponse(t, mode, true)
			if response.Response != yagoproto.QueryResponseRejected {
				t.Fatalf("Response = %d, want rejected", response.Response)
			}
		})
	}
}

func mountedQueryNetworkResponse(
	t *testing.T,
	mode yagoproto.NetworkAuthenticationMode,
	networkPresent bool,
) yagoproto.QueryResponse {
	t.Helper()

	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedQueryStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	identity := nodeidentity.Identity{
		Hash:                     yagomodel.WordHash("self"),
		NetworkName:              "freeworld",
		AuthenticationMode:       mode,
		AuthenticationEssentials: "shared-secret",
	}
	MountQuery(router, identity, stubCounter{rwi: 11})
	form := queryRequest(yagoproto.ObjectRWICount).Form()
	form.Del(yagoproto.FieldNetworkName)
	if networkPresent {
		form.Set(yagoproto.FieldNetworkName, "")
	}
	if mode == yagoproto.NetworkAuthenticationSaltedMagic {
		form.Set(yagoproto.FieldKey, "salt1234")
		form.Set(yagoproto.FieldIam, "opaque-caller")
		form.Set(
			yagoproto.FieldMagicMD5,
			yagoproto.MagicMD5("salt1234", "opaque-caller", "shared-secret"),
		)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathQuery+"?"+form.Encode(),
		nil,
	)
	mux.ServeHTTP(recorder, request)

	message, err := yagomodel.ParseMessage(recorder.Body.String())
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	response, err := yagoproto.ParseQueryResponse(message)
	if err != nil {
		t.Fatalf("parse query response: %v", err)
	}

	return response
}

func TestMountQueryUnknownObjectUsesTemplateMarker(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedQueryStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	identity := nodeidentity.Identity{
		Hash:        yagomodel.WordHash("self"),
		NetworkName: "freeworld",
		Start:       time.UnixMilli(123456789),
	}
	MountQuery(router, identity, stubCounter{rwi: 11})
	form := queryRequest(yagoproto.QueryObject("unknown")).Form()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathQuery+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	message, err := yagomodel.ParseMessage(recorder.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	if message[yagoproto.FieldResponse] != yagoproto.QueryResponseUnresolved ||
		message[yagoproto.FieldMyTime] == yagoproto.QueryResponseUnresolved ||
		message[yagoproto.FieldMagic] != "123456789" {
		t.Fatalf("message = %#v", message)
	}
}

func TestMountQueryToleratesArbitraryTargetAndEmitsUnresolvedTime(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedQueryStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	identity := nodeidentity.Identity{
		Hash:        yagomodel.WordHash("self"),
		NetworkName: "freeworld",
	}
	MountQuery(router, identity, stubCounter{rwi: 11})
	form := queryRequest(yagoproto.ObjectRWICount).Form()
	form.Set(yagoproto.FieldNetworkName, "other")
	form.Set(yagoproto.FieldYouAre, "not-a-hash")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathQuery+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	message, err := yagomodel.ParseMessage(recorder.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	if message[yagoproto.FieldResponse] != "-1" ||
		message[yagoproto.FieldMyTime] != yagoproto.QueryResponseUnresolved {
		t.Fatalf("message = %#v", message)
	}
}
