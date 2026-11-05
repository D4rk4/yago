package httpguard_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

func captureRequestAcceptedLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &output
}

func TestParseDoesNotLogWholeForm(t *testing.T) {
	guard := httpguard.NewRequestGuard(1<<20, time.Second)
	rec := httptest.NewRecorder()
	form := url.Values{}
	form.Set("key", "synthetic-peer-key-0001")
	form.Set("magic", "synthetic-magic-token-0002")
	form.Set("payload", "synthetic-transfer-payload-0003")
	body := strings.NewReader(form.Encode())
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathTransferURL,
		body,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	output := captureRequestAcceptedLogs(t)
	_, _, _, ok := guard.Parse(rec, req, yagoproto.TransferURLEndpointMethods)
	if !ok {
		t.Fatal("Parse rejected a valid request")
	}

	logged := output.String()
	if !strings.Contains(logged, `"msg":"request accepted"`) {
		t.Fatalf("accepted request was not logged; got:\n%s", logged)
	}
	for _, unwanted := range []string{
		"synthetic-peer-key-0001",
		"synthetic-magic-token-0002",
		"synthetic-transfer-payload-0003",
	} {
		if strings.Contains(logged, unwanted) {
			t.Fatalf("log line contained sensitive form value %q", unwanted)
		}
	}
}

func TestParseLogsBoundedMetadata(t *testing.T) {
	guard := httpguard.NewRequestGuard(1<<20, time.Second)
	rec := httptest.NewRecorder()
	form := url.Values{}
	form.Set("url0", "https://example.com/a")
	form.Set("url1", "https://example.com/b")
	body := strings.NewReader(form.Encode())
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathTransferURL,
		body,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ContentLength = int64(body.Len())

	output := captureRequestAcceptedLogs(t)
	_, _, _, ok := guard.Parse(rec, req, yagoproto.TransferURLEndpointMethods)
	if !ok {
		t.Fatal("Parse rejected a valid request")
	}

	logged := output.String()
	for _, want := range []string{
		`"msg":"request accepted"`,
		`"method":"POST"`,
		`"path":"` + yagoproto.PathTransferURL + `"`,
		`"formFields":2`,
		`"bodyBytes":` + strconv.Itoa(len(form.Encode())),
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log line missing %q; got:\n%s", want, logged)
		}
	}
}
