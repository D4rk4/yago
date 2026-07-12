//go:build e2e

package e2e

import (
	"context"

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
	return peerQueryCountWithEnv(ctx, probe, peerURL, hash, object, "")
}

func peerQueryCountWithEnv(
	ctx context.Context,
	probe *httpProbe,
	peerURL string,
	hash yagomodel.Hash,
	object yagoproto.QueryObject,
	env string,
) (int, bool) {
	queryURL := peerURL + "/yacy/query.html?" + (yagoproto.QueryRequest{
		NetworkName: yagoproto.DefaultNetwork,
		YouAre:      hash,
		Object:      object,
		Env:         env,
	}).Form().Encode()
	result := probe.Get(ctx, queryURL)
	if !result.ok {
		return 0, false
	}
	return queryResponseCount(result.body)
}
