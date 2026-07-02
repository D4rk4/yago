//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

func restartYaCy(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	container testcontainers.Container,
) string {
	t.Helper()
	stopTimeout := 30 * time.Second
	if err := container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop yacy: %v", err)
	}
	if err := container.Start(ctx); err != nil {
		t.Fatalf("restart yacy: %v", err)
	}
	yacyURL := hostURL(t, ctx, container)
	if !waitFor(180*time.Second, func() bool {
		return probe.OK(ctx, yacyURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("YaCy never became reachable after restart")
	}
	return yacyURL
}
