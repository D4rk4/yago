//go:build e2e

package e2e

import (
	"context"
	"net/url"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func peerQueryCount(
	ctx context.Context,
	probe *httpProbe,
	peerURL string,
	hash yacymodel.Hash,
	object yacyproto.QueryObject,
) (int, bool) {
	queryURL := peerURL + "/yacy/query.html?" + url.Values{
		yacyproto.FieldNetworkName: {yacyproto.DefaultNetwork},
		yacyproto.FieldYouAre:      {hash.String()},
		yacyproto.FieldObject:      {string(object)},
	}.Encode()
	result := probe.Get(ctx, queryURL)
	if !result.ok {
		return 0, false
	}
	return queryResponseCount(result.body)
}
