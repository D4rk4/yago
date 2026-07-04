package yacywire_test

import (
	"fmt"
	"net/url"
	"os"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

var fixtureReaders = map[string]func() ([]byte, error){
	"hello-request.properties":         readHelloRequestFixture,
	"hello-response.properties":        readHelloResponseFixture,
	"query-request.properties":         readQueryRequestFixture,
	"query-response.properties":        readQueryResponseFixture,
	"transfer-rwi-request.properties":  readTransferRWIRequestFixture,
	"transfer-rwi-response.properties": readTransferRWIResponseFixture,
	"transfer-url-request.properties":  readTransferURLRequestFixture,
	"transfer-url-response.properties": readTransferURLResponseFixture,
	"search-request.properties":        readSearchRequestFixture,
	"search-response.properties":       readSearchResponseFixture,
}

func TestHelloWireFixtures(t *testing.T) {
	t.Parallel()

	form := loadFixtureForm(t, "hello-request.properties")
	req, err := yagoproto.ParseHelloRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseHelloRequest: %v", err)
	}
	gotReq, err := yagoproto.ParseHelloRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatalf("ParseHelloRequest encoded: %v", err)
	}
	if !reflect.DeepEqual(gotReq, req) {
		t.Fatalf("request round-trip:\n got %#v\nwant %#v", gotReq, req)
	}

	msg := loadFixtureMessage(t, "hello-response.properties")
	resp, err := yagoproto.ParseHelloResponse(t.Context(), msg)
	if err != nil {
		t.Fatalf("ParseHelloResponse: %v", err)
	}
	encoded := resp.Encode()
	yagoproto.InjectResponseHeader(encoded, resp.Version, resp.Uptime)
	gotResp, err := yagoproto.ParseHelloResponse(t.Context(), encoded)
	if err != nil {
		t.Fatalf("ParseHelloResponse encoded: %v", err)
	}
	if !reflect.DeepEqual(gotResp, resp) {
		t.Fatalf("response round-trip:\n got %#v\nwant %#v", gotResp, resp)
	}
}

func TestQueryWireFixtures(t *testing.T) {
	t.Parallel()

	form := loadFixtureForm(t, "query-request.properties")
	req, err := yagoproto.ParseQueryRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseQueryRequest: %v", err)
	}
	gotReq, err := yagoproto.ParseQueryRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatalf("ParseQueryRequest encoded: %v", err)
	}
	if !reflect.DeepEqual(gotReq, req) {
		t.Fatalf("request round-trip:\n got %#v\nwant %#v", gotReq, req)
	}

	msg := loadFixtureMessage(t, "query-response.properties")
	resp, err := yagoproto.ParseQueryResponse(msg)
	if err != nil {
		t.Fatalf("ParseQueryResponse: %v", err)
	}
	encoded := resp.Encode()
	yagoproto.InjectResponseHeader(encoded, resp.Version, resp.Uptime)
	gotResp, err := yagoproto.ParseQueryResponse(encoded)
	if err != nil {
		t.Fatalf("ParseQueryResponse encoded: %v", err)
	}
	if !reflect.DeepEqual(gotResp, resp) {
		t.Fatalf("response round-trip:\n got %#v\nwant %#v", gotResp, resp)
	}
}

func TestTransferRWIWireFixtures(t *testing.T) {
	t.Parallel()

	form := loadFixtureForm(t, "transfer-rwi-request.properties")
	req, err := yagoproto.ParseTransferRWIRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest: %v", err)
	}
	gotReq, err := yagoproto.ParseTransferRWIRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatalf("ParseTransferRWIRequest encoded: %v", err)
	}
	if !reflect.DeepEqual(gotReq, req) {
		t.Fatalf("request round-trip:\n got %#v\nwant %#v", gotReq, req)
	}

	msg := loadFixtureMessage(t, "transfer-rwi-response.properties")
	resp, err := yagoproto.ParseTransferRWIResponse(msg)
	if err != nil {
		t.Fatalf("ParseTransferRWIResponse: %v", err)
	}
	encoded := resp.Encode()
	yagoproto.InjectResponseHeader(encoded, resp.Version, resp.Uptime)
	gotResp, err := yagoproto.ParseTransferRWIResponse(encoded)
	if err != nil {
		t.Fatalf("ParseTransferRWIResponse encoded: %v", err)
	}
	if !reflect.DeepEqual(gotResp, resp) {
		t.Fatalf("response round-trip:\n got %#v\nwant %#v", gotResp, resp)
	}
}

func TestTransferURLWireFixtures(t *testing.T) {
	t.Parallel()

	form := loadFixtureForm(t, "transfer-url-request.properties")
	req, err := yagoproto.ParseTransferURLRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseTransferURLRequest: %v", err)
	}
	gotReq, err := yagoproto.ParseTransferURLRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatalf("ParseTransferURLRequest encoded: %v", err)
	}
	if !reflect.DeepEqual(gotReq, req) {
		t.Fatalf("request round-trip:\n got %#v\nwant %#v", gotReq, req)
	}

	msg := loadFixtureMessage(t, "transfer-url-response.properties")
	resp, err := yagoproto.ParseTransferURLResponse(msg)
	if err != nil {
		t.Fatalf("ParseTransferURLResponse: %v", err)
	}
	encoded := resp.Encode()
	yagoproto.InjectResponseHeader(encoded, resp.Version, resp.Uptime)
	gotResp, err := yagoproto.ParseTransferURLResponse(encoded)
	if err != nil {
		t.Fatalf("ParseTransferURLResponse encoded: %v", err)
	}
	if !reflect.DeepEqual(gotResp, resp) {
		t.Fatalf("response round-trip:\n got %#v\nwant %#v", gotResp, resp)
	}
}

func TestSearchWireFixtures(t *testing.T) {
	t.Parallel()

	form := loadFixtureForm(t, "search-request.properties")
	req, err := yagoproto.ParseSearchRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("ParseSearchRequest: %v", err)
	}
	gotReq, err := yagoproto.ParseSearchRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatalf("ParseSearchRequest encoded: %v", err)
	}
	if !reflect.DeepEqual(gotReq, req) {
		t.Fatalf("request round-trip:\n got %#v\nwant %#v", gotReq, req)
	}

	msg := loadFixtureMessage(t, "search-response.properties")
	resp, err := yagoproto.ParseSearchResponse(msg)
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}
	encoded := resp.Encode()
	yagoproto.InjectResponseHeader(encoded, resp.Version, resp.Uptime)
	gotResp, err := yagoproto.ParseSearchResponse(encoded)
	if err != nil {
		t.Fatalf("ParseSearchResponse encoded: %v", err)
	}
	if !reflect.DeepEqual(gotResp, resp) {
		t.Fatalf("response round-trip:\n got %#v\nwant %#v", gotResp, resp)
	}
}

func loadFixtureForm(tb testing.TB, name string) url.Values {
	tb.Helper()

	msg := loadFixtureMessage(tb, name)
	form := url.Values{}
	for key, value := range msg {
		form.Set(key, value)
	}

	return form
}

func loadFixtureMessage(tb testing.TB, name string) yagomodel.Message {
	tb.Helper()

	raw, err := loadFixture(tb, name)
	if err != nil {
		tb.Fatalf("load fixture %s: %v", name, err)
	}

	msg, err := yagomodel.ParseMessage(string(raw))
	if err != nil {
		tb.Fatalf("parse fixture %s: %v", name, err)
	}

	return msg
}

func loadFixture(tb testing.TB, name string) ([]byte, error) {
	tb.Helper()

	read, ok := fixtureReaders[name]
	if !ok {
		tb.Fatalf("unknown fixture %s", name)
	}

	return read()
}

func readHelloRequestFixture() ([]byte, error) {
	raw, err := os.ReadFile("hello-request.properties")
	if err != nil {
		return nil, fmt.Errorf("read hello-request.properties: %w", err)
	}
	return raw, nil
}

func readHelloResponseFixture() ([]byte, error) {
	raw, err := os.ReadFile("hello-response.properties")
	if err != nil {
		return nil, fmt.Errorf("read hello-response.properties: %w", err)
	}
	return raw, nil
}

func readQueryRequestFixture() ([]byte, error) {
	raw, err := os.ReadFile("query-request.properties")
	if err != nil {
		return nil, fmt.Errorf("read query-request.properties: %w", err)
	}
	return raw, nil
}

func readQueryResponseFixture() ([]byte, error) {
	raw, err := os.ReadFile("query-response.properties")
	if err != nil {
		return nil, fmt.Errorf("read query-response.properties: %w", err)
	}
	return raw, nil
}

func readTransferRWIRequestFixture() ([]byte, error) {
	raw, err := os.ReadFile("transfer-rwi-request.properties")
	if err != nil {
		return nil, fmt.Errorf("read transfer-rwi-request.properties: %w", err)
	}
	return raw, nil
}

func readTransferRWIResponseFixture() ([]byte, error) {
	raw, err := os.ReadFile("transfer-rwi-response.properties")
	if err != nil {
		return nil, fmt.Errorf("read transfer-rwi-response.properties: %w", err)
	}
	return raw, nil
}

func readTransferURLRequestFixture() ([]byte, error) {
	raw, err := os.ReadFile("transfer-url-request.properties")
	if err != nil {
		return nil, fmt.Errorf("read transfer-url-request.properties: %w", err)
	}
	return raw, nil
}

func readTransferURLResponseFixture() ([]byte, error) {
	raw, err := os.ReadFile("transfer-url-response.properties")
	if err != nil {
		return nil, fmt.Errorf("read transfer-url-response.properties: %w", err)
	}
	return raw, nil
}

func readSearchRequestFixture() ([]byte, error) {
	raw, err := os.ReadFile("search-request.properties")
	if err != nil {
		return nil, fmt.Errorf("read search-request.properties: %w", err)
	}
	return raw, nil
}

func readSearchResponseFixture() ([]byte, error) {
	raw, err := os.ReadFile("search-response.properties")
	if err != nil {
		return nil, fmt.Errorf("read search-response.properties: %w", err)
	}
	return raw, nil
}
