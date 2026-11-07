//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func postNodeRWILines(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
	nodeHash yagomodel.Hash,
	wordCount int,
	lines []string,
) {
	t.Helper()
	rwiForm := url.Values{
		yagoproto.FieldNetworkName: {yagoproto.DefaultNetwork},
		yagoproto.FieldIam:         {seededPeerHash.String()},
		yagoproto.FieldYouAre:      {nodeHash.String()},
		yagoproto.FieldWordCount:   {strconv.Itoa(wordCount)},
		yagoproto.FieldEntryCount:  {strconv.Itoa(len(lines))},
		yagoproto.FieldIndexes:     {strings.Join(lines, "\n")},
		"key":                      {"e2e-salt"},
	}
	result := probe.PostRaw(
		ctx,
		nodeURL+"/yacy/transferRWI.html",
		rwiForm.Encode(),
		"Content-Type: application/x-www-form-urlencoded",
	)
	if !result.ok || !strings.Contains(result.body, "result=ok") {
		t.Fatalf("transferRWI to node failed: %s", result.diag())
	}
}

func postNodeURLRows(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
	nodeHash yagomodel.Hash,
	rows []string,
) {
	t.Helper()
	urlForm := url.Values{
		yagoproto.FieldNetworkName: {yagoproto.DefaultNetwork},
		yagoproto.FieldIam:         {seededPeerHash.String()},
		yagoproto.FieldYouAre:      {nodeHash.String()},
		yagoproto.FieldURLCount:    {strconv.Itoa(len(rows))},
	}
	for position, row := range rows {
		urlForm.Set(fmt.Sprintf("url%d", position), row)
	}
	result := probe.PostRaw(
		ctx,
		nodeURL+"/yacy/transferURL.html",
		urlForm.Encode(),
		"Content-Type: application/x-www-form-urlencoded",
	)
	if !result.ok || !strings.Contains(result.body, "result=ok") {
		t.Fatalf("transferURL to node failed: %s", result.diag())
	}
}

func seededURLRow(urlHash yagomodel.URLHash, pageURL, title string, wordCount int) string {
	return fmt.Sprintf(
		"{descr=%s,flags=AAAAAA,fresh=20260702,hash=%s,load=20260702,mod=20260702,size=512,url=%s,wc=%d}",
		yagomodel.EncodeBase64WireForm(title),
		urlHash,
		yagomodel.EncodeBase64WireForm(pageURL),
		wordCount,
	)
}
