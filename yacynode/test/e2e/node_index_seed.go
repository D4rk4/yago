//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

const seededPeerHash = yacymodel.Hash("QRSTUVWXYZab")

func seedNodeIndex(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
	nodeHash yacymodel.Hash,
	words []string,
	pageURL string,
) {
	t.Helper()
	urlHash, err := yacymodel.HashURL(pageURL)
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}

	lines := make([]string, len(words))
	for i, word := range words {
		lines[i] = fmt.Sprintf(
			"%s{c=1,h=%s,x=2,z=AAAAAA}",
			yacymodel.WordHash(word),
			urlHash,
		)
	}

	rwiForm := url.Values{
		yacyproto.FieldNetworkName: {yacyproto.DefaultNetwork},
		yacyproto.FieldIam:         {seededPeerHash.String()},
		yacyproto.FieldYouAre:      {nodeHash.String()},
		yacyproto.FieldWordCount:   {fmt.Sprintf("%d", len(words))},
		yacyproto.FieldEntryCount:  {fmt.Sprintf("%d", len(words))},
		yacyproto.FieldIndexes:     {strings.Join(lines, "\n")},
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
		yacymodel.EncodeBase64WireForm("Transfer Interop Document"),
		urlHash,
		yacymodel.EncodeBase64WireForm(pageURL),
		len(words),
	)
	urlForm := url.Values{
		yacyproto.FieldNetworkName: {yacyproto.DefaultNetwork},
		yacyproto.FieldIam:         {seededPeerHash.String()},
		yacyproto.FieldYouAre:      {nodeHash.String()},
		yacyproto.FieldURLCount:    {"1"},
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
