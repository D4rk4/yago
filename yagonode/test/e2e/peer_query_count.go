//go:build e2e

package e2e

import (
	"context"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func peerQueryCount(
	ctx context.Context,
	probe *httpProbe,
	peerURL string,
	hash yagomodel.Hash,
	object yagoproto.QueryObject,
) (int, bool) {
	queryURL := peerURL + "/yacy/query.html?" + url.Values{
		yagoproto.FieldNetworkName: {yagoproto.DefaultNetwork},
		yagoproto.FieldYouAre:      {hash.String()},
		yagoproto.FieldObject:      {string(object)},
	}.Encode()
	result := probe.Get(ctx, queryURL)
	if !result.ok {
		return 0, false
	}
	return queryResponseCount(result.body)
}
