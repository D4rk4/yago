//go:build e2e

package e2e

import (
	"context"
	"encoding/xml"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func yaCyURLMetadataContains(
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	callerHash yagomodel.Hash,
	yacyHash yagomodel.Hash,
	urlHash yagomodel.Hash,
) bool {
	return len(
		yaCyStoredURLMetadata(ctx, probe, yacyURL, callerHash, yacyHash, []yagomodel.Hash{urlHash}),
	) == 1
}

func yaCyStoredURLMetadata(
	ctx context.Context,
	probe *httpProbe,
	yacyURL string,
	callerHash yagomodel.Hash,
	yacyHash yagomodel.Hash,
	urlHashes []yagomodel.Hash,
) map[yagomodel.Hash]bool {
	var requested strings.Builder
	for _, urlHash := range urlHashes {
		requested.WriteString(urlHash.String())
	}
	request := yagoproto.CrawlURLRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Iam:         callerHash.String(),
		YouAre:      yacyHash.String(),
		Call:        yagoproto.CrawlURLCallURLHashList,
		Hashes:      requested.String(),
	}
	stored := map[yagomodel.Hash]bool{}
	result := probe.Get(ctx, yacyURL+yagoproto.PathCrawlURLs+"?"+request.Form().Encode())
	if !result.ok {
		return stored
	}

	var feed struct {
		YaCy struct {
			Response string `xml:"response"`
		} `xml:"yacy"`
		Channel struct {
			Items []struct {
				GUID string `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal([]byte(result.body), &feed); err != nil ||
		feed.YaCy.Response != yagoproto.CrawlURLResponseOK {
		return stored
	}
	for _, urlHash := range urlHashes {
		for _, item := range feed.Channel.Items {
			if item.GUID == urlHash.String() {
				stored[urlHash] = true
			}
		}
	}

	return stored
}
