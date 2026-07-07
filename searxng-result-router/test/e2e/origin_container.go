//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/staticpage"
)

const (
	originAlias = "origin"
	originPage  = `<html lang="en"><title>Origin</title><body>origin page</body></html>`
)

func startOrigin(t *testing.T, ctx context.Context, networkName string) {
	t.Helper()
	staticpage.Start(t, ctx, networkName, originAlias, originPage)
}

func originNetworkURL() string {
	return "http://" + originAlias + "/"
}
