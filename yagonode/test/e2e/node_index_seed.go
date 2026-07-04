//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const seededPeerHash = yagomodel.Hash("QRSTUVWXYZab")

func seedNodeIndex(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
	nodeHash yagomodel.Hash,
	words []string,
	pageURL string,
) {
	t.Helper()
	urlHash, err := yagomodel.HashURL(pageURL)
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}

	lines := make([]string, len(words))
	for i, word := range words {
		lines[i] = fmt.Sprintf(
			"%s{c=1,h=%s,x=2,z=AAAAAA}",
			yagomodel.WordHash(word),
			urlHash,
		)
	}

	rwiForm := url.Values{
		yagoproto.FieldNetworkName: {yagoproto.DefaultNetwork},
		yagoproto.FieldIam:         {seededPeerHash.String()},
		yagoproto.FieldYouAre:      {nodeHash.String()},
		yagoproto.FieldWordCount:   {fmt.Sprintf("%d", len(words))},
		yagoproto.FieldEntryCount:  {fmt.Sprintf("%d", len(words))},
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

	row := fmt.Sprintf(
		"{descr=%s,flags=AAAAAA,fresh=20260702,hash=%s,load=20260702,mod=20260702,size=512,url=%s,wc=%d}",
		yagomodel.EncodeBase64WireForm("Transfer Interop Document"),
		urlHash,
		yagomodel.EncodeBase64WireForm(pageURL),
		len(words),
	)
	urlForm := url.Values{
		yagoproto.FieldNetworkName: {yagoproto.DefaultNetwork},
		yagoproto.FieldIam:         {seededPeerHash.String()},
		yagoproto.FieldYouAre:      {nodeHash.String()},
		yagoproto.FieldURLCount:    {"1"},
		"url0":                     {row},
	}
	result = probe.PostRaw(
		ctx,
		nodeURL+"/yacy/transferURL.html",
		urlForm.Encode(),
		"Content-Type: application/x-www-form-urlencoded",
	)
	if !result.ok || !strings.Contains(result.body, "result=ok") {
		t.Fatalf("transferURL to node failed: %s", result.diag())
	}
}
