//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	largeTransferDocumentCount = 120
	largeTransferTitleBytes    = 1600
	javaFormContentLimitBytes  = 200000
)

type largeTransferDocument struct {
	urlHash yagomodel.URLHash
	row     string
}

func buildLargeTransferDocuments(t *testing.T) []largeTransferDocument {
	t.Helper()
	documents := make([]largeTransferDocument, largeTransferDocumentCount)
	rowBytes := 0
	for position := range documents {
		pageURL := fmt.Sprintf("http://large-transfer.example.com/document-%03d.txt", position)
		urlHash, err := yagomodel.HashURL(pageURL)
		if err != nil {
			t.Fatalf("hash large transfer URL: %v", err)
		}
		phrase := fmt.Sprintf("synthetic large transfer title %03d ", position)
		title := strings.Repeat(phrase, largeTransferTitleBytes/len(phrase)+1)[:largeTransferTitleBytes]
		row := seededURLRow(urlHash, pageURL, title, 1)
		documents[position] = largeTransferDocument{urlHash: urlHash, row: row}
		rowBytes += len(row)
	}
	if rowBytes <= javaFormContentLimitBytes {
		t.Fatalf(
			"large transfer rows = %d bytes, want above %d",
			rowBytes,
			javaFormContentLimitBytes,
		)
	}

	return documents
}

func seedNodeLargeTransfer(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
	nodeHash yagomodel.Hash,
	word yagomodel.Hash,
	documents []largeTransferDocument,
) {
	t.Helper()
	announceIndexTransferSender(t, ctx, probe, nodeURL)
	lines := make([]string, len(documents))
	rows := make([]string, len(documents))
	for position, document := range documents {
		lines[position] = fmt.Sprintf("%s{c=1,h=%s,t=1,x=2,z=AAAAAA}", word, document.urlHash)
		rows[position] = document.row
	}
	postNodeRWILines(t, ctx, probe, nodeURL, nodeHash, 1, lines)
	postNodeURLRows(t, ctx, probe, nodeURL, nodeHash, rows)
}
