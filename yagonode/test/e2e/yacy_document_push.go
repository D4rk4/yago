//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"testing"
	"time"
)

const yacyAdminAuthHeader = "Authorization: Basic YWRtaW46eWFjeQ=="

func buildTransferTokens() []string {
	tokens := make([]string, 150)
	for i := range tokens {
		tokens[i] = fmt.Sprintf("yacyrwitransferuniquetoken%03d", i)
	}
	return tokens
}

func pushDocument(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	tokens []string,
) {
	t.Helper()
	wantURL := fmt.Sprintf(
		"http://transfer.example.invalid/doc-%d-%s.txt",
		len(tokens),
		tokens[0],
	)

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

	var result probeResult
	pushed := waitForEvery(30*time.Second, time.Second, func() bool {
		result = probe.PostRaw(ctx, yacyURL+"/api/push_p.json", body,
			"Content-Type: "+contentType, yacyAdminAuthHeader)

		return result.ok && strings.Contains(result.body, "successall")
	})
	if !pushed {
		t.Fatalf("push_p.json request to YaCy failed: %s", result.diag())
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
