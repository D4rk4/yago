package yagonode

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetfetch"
)

const (
	snippetSlowPDFStreams     = 64
	snippetSlowPDFStreamBytes = 512 << 10
	snippetEnrichmentPrompt   = 450 * time.Millisecond
)

func snippetSlowPDF(t *testing.T) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	operators := bytes.Repeat([]byte("5 0 Td "), snippetSlowPDFStreamBytes/7)
	if _, err := writer.Write(operators); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	var pdf, references bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	for index := range snippetSlowPDFStreams {
		fmt.Fprintf(&references, "%d 0 R ", index+2)
		fmt.Fprintf(&pdf, "%d 0 obj\n<< /Filter /FlateDecode /Length %d >>\nstream\n",
			index+2, compressed.Len())
		pdf.Write(compressed.Bytes())
		pdf.WriteString("\nendstream\nendobj\n")
	}
	pdf.WriteString(
		"1 0 obj\n<< /Type /Page /Contents [" + references.String() + "] >>\nendobj\n%%EOF",
	)

	return pdf.Bytes()
}

func snippetReadablePDF(text string) []byte {
	content := "BT (" + text + ") Tj ET"

	return fmt.Appendf(nil,
		"%%PDF-1.4\n1 0 obj\n<< /Type /Page /Contents 2 0 R >>\nendobj\n"+
			"2 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n%%%%EOF",
		len(content), content)
}

func snippetPDFServer(t *testing.T) *httptest.Server {
	t.Helper()
	pages := map[string][]byte{
		"/slow.pdf":    snippetSlowPDF(t),
		"/survey.pdf":  snippetReadablePDF("Tidal harbour survey of the northern pier"),
		"/orchard.pdf": snippetReadablePDF("Apple orchard pruning calendar for winter"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.Copy(w, bytes.NewReader(pages[r.URL.Path]))
	}))
	t.Cleanup(server.Close)

	return server
}

func searchRemotePDF(t *testing.T, server *httptest.Server, path string) searchcore.Response {
	t.Helper()
	search := snippetfetch.WithSnippetEnrichment(staticSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title: "Survey", URL: server.URL + path, Snippet: "Survey",
			Source: searchcore.SourceRemote,
		}},
	}}, buildSnippetEnricher(nodeConfig{PeerSnippetFetch: true}, server.Client()), remoteTextEvidence)
	response, err := search.Search(t.Context(), searchcore.Request{Terms: []string{"harbour"}})
	if err != nil {
		t.Fatalf("Search %s: %v", path, err)
	}

	return response
}

func TestSnippetEnrichmentStopsReadingASlowPDFAtItsBudget(t *testing.T) {
	server := snippetPDFServer(t)
	started := time.Now()
	response := searchRemotePDF(t, server, "/slow.pdf")
	elapsed := time.Since(started)
	if elapsed > snippetEnrichmentPrompt {
		t.Fatalf("snippet enrichment waited %v for a slow PDF", elapsed)
	}
	if len(response.Results) != 0 {
		t.Fatalf("unverified slow PDF result kept: %+v", response.Results)
	}
	response = searchRemotePDF(t, server, "/survey.pdf")
	if len(response.Results) != 1 || !strings.Contains(response.Results[0].Snippet, "harbour") {
		t.Fatalf("readable PDF evidence = %+v", response.Results)
	}
}

func TestSnippetEnrichmentDropsAReadablePDFWithoutTheTermWithoutFailing(t *testing.T) {
	response := searchRemotePDF(t, snippetPDFServer(t), "/orchard.pdf")
	if len(response.Results) != 0 || response.TotalResults != 0 {
		t.Fatalf("PDF without the term = %+v", response)
	}
}
