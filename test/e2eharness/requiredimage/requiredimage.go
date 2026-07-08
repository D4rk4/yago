//go:build e2e

package requiredimage

import (
	"os"
	"testing"
)

func FromEnv(t *testing.T, envVar, component, makeTarget string) string {
	t.Helper()
	image := os.Getenv(envVar)
	if image == "" {
		t.Fatalf(
			"%s is not set; build the %s image first (run via `make %s`)",
			envVar, component, makeTarget,
		)
	}
	return image
}
