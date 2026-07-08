//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/staticpage"
)

const (
	originAlias        = "origin"
	renderedMarker     = "rendered-by-browser-marker"
	scriptedOriginPage = `<html lang="en"><title>Hi</title><body>` +
		`<script>document.body.appendChild(document.createTextNode("` + renderedMarker + `"));</script>` +
		`</body></html>`
)

func startScriptedOrigin(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	return staticpage.Start(t, ctx, networkName, originAlias, scriptedOriginPage)
}
