//go:build e2e

package manticore

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/pollwait"
)

const (
	writeReadinessProbeTable = "manticore_write_readiness_probe"
	writeReadinessProbeBody  = `{"table":"` + writeReadinessProbeTable + `","id":1,"doc":{"content":"probe"}}`
	writeReadinessTimeout    = 2 * time.Minute
)

func awaitWritePathReady(t *testing.T, ctx context.Context, hostURL string) {
	t.Helper()
	if !pollwait.For(writeReadinessTimeout, func() bool {
		return writeProbeAccepted(ctx, hostURL)
	}) {
		t.Fatalf("manticore write path not ready within %s", writeReadinessTimeout)
	}
	dropProbeTable(ctx, hostURL)
}

func writeProbeAccepted(ctx context.Context, hostURL string) bool {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, hostURL+"/replace", strings.NewReader(writeReadinessProbeBody),
	)
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode < 300
}

func dropProbeTable(ctx context.Context, hostURL string) {
	query := url.Values{"query": {"DROP TABLE IF EXISTS " + writeReadinessProbeTable}}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, hostURL+"/sql?mode=raw&"+query.Encode(), nil,
	)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
