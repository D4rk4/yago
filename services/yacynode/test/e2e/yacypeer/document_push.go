//go:build e2e

package yacypeer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/httpprobe"
)

const adminAuthHeader = "Authorization: Basic YWRtaW46eWFjeQ=="

func TransferTokens() []string {
	tokens := make([]string, 150)
	for i := range tokens {
		tokens[i] = fmt.Sprintf("yacyrwitransferuniquetoken%03d", i)
	}
	return tokens
}

func PushDocument(
	t *testing.T,
	ctx context.Context,
	probe *httpprobe.Probe,
	yacyURL string,
	tokens []string,
) {
	t.Helper()
	wantURL := fmt.Sprintf("http://transfer.example.invalid/doc-%d.txt", len(tokens))

	body, contentType := buildMultipart(
		map[string]string{
			"count":         "1",
			"url-0":         wantURL,
			"contentType-0": "text/plain",
			"collection-0":  "transfer",
			"synchronous":   "true",
			"commit":        "true",
		},
		map[string]string{"data-0": strings.Join(tokens, " ")},
	)

	result := probe.PostRaw(ctx, yacyURL+"/api/push_p.json", body,
		"Content-Type: "+contentType, adminAuthHeader)
	if !result.OK {
		t.Fatalf("push_p.json request to YaCy failed: %s", result.Diag())
	}
	if !strings.Contains(result.Body, "successall") {
		t.Fatalf("push_p.json did not report success: %s", result.Body)
	}
}

func buildMultipart(fields, files map[string]string) (body, contentType string) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		_ = writer.WriteField(key, value)
	}
	for key, value := range files {
		formWriter, _ := writer.CreateFormFile(key, key)
		_, _ = io.WriteString(formWriter, value)
	}
	_ = writer.Close()
	return buf.String(), writer.FormDataContentType()
}
