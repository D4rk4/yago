package yagonode

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseContainerPublicVerificationUsesProtectedEnvironment(t *testing.T) {
	contents, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	workflow := string(contents)
	publishStart := strings.Index(workflow, "\n  container_publish:\n")
	publicStart := strings.Index(workflow, "\n  container_public_verify:\n")
	releaseStart := strings.Index(workflow, "\n  release:\n")
	if publishStart < 0 || publicStart <= publishStart || releaseStart <= publicStart {
		t.Fatal("release workflow container publication jobs are not ordered")
	}

	publishJob := workflow[publishStart:publicStart]
	publicJob := workflow[publicStart:releaseStart]
	for _, required := range []string{
		`node_digest: ${{ steps.publish.outputs.node_digest }}`,
		`crawler_digest: ${{ steps.publish.outputs.crawler_digest }}`,
		`Verify container manifest attestations`,
	} {
		if !strings.Contains(publishJob, required) {
			t.Fatalf("release container publication missing %q", required)
		}
	}
	for _, required := range []string{
		`needs: container_publish`,
		`name: release-container-public-visibility`,
		`url: https://github.com/users/D4rk4/packages/container/yago-crawler/settings`,
		`NODE_DIGEST: ${{ needs.container_publish.outputs.node_digest }}`,
		`CRAWLER_DIGEST: ${{ needs.container_publish.outputs.crawler_digest }}`,
		`Verify public release containers without credentials`,
		`verify-public-release-containers.sh`,
	} {
		if !strings.Contains(publicJob, required) {
			t.Fatalf("release public container gate missing %q", required)
		}
	}
	if strings.Contains(publishJob, `verify-public-release-containers.sh`) {
		t.Fatal(
			"authenticated publication attempts public verification before environment approval",
		)
	}
	if !strings.Contains(workflow[releaseStart:], `needs: [build, container_public_verify]`) {
		t.Fatal("GitHub Release publication does not depend on the public container gate")
	}
}
