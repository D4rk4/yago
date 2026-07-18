//go:build e2e

package e2e

import (
	"context"
	"encoding/xml"

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
	request := yagoproto.CrawlURLRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Iam:         callerHash.String(),
		YouAre:      yacyHash.String(),
		Call:        yagoproto.CrawlURLCallURLHashList,
		Hashes:      urlHash.String(),
	}
	result := probe.Get(ctx, yacyURL+yagoproto.PathCrawlURLs+"?"+request.Form().Encode())
	if !result.ok {
		return false
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
		return false
	}
	for _, item := range feed.Channel.Items {
		if item.GUID == urlHash.String() {
			return true
		}
	}

	return false
}
