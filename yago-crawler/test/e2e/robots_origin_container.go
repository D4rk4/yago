//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	robotsOriginAlias = "robotsorigin"
	robotsOriginPage  = `<html lang="en"><title>Hi</title><body>sitemap discovered words</body></html>`
	robotsFileBody    = "User-agent: *\nSitemap: http://" + robotsOriginAlias + "/sitemap.xml\n"
	robotsSitemapBody = `<urlset><url><loc>http://` + robotsOriginAlias + `/</loc></url></urlset>`
)

func startRobotsOrigin(t *testing.T, ctx context.Context, networkName string) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          originImage,
			ExposedPorts:   []string{"80/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {robotsOriginAlias}},
			Files: []testcontainers.ContainerFile{
				{
					Reader:            strings.NewReader(robotsOriginPage),
					ContainerFilePath: "/usr/share/nginx/html/index.html",
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader(robotsFileBody),
					ContainerFilePath: "/usr/share/nginx/html/robots.txt",
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader(robotsSitemapBody),
					ContainerFilePath: "/usr/share/nginx/html/sitemap.xml",
					FileMode:          0o644,
				},
			},
			WaitingFor: wait.ForHTTP("/robots.txt").WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start robots origin container %s: %v", originImage, err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "robotsorigin", container)
	return "http://" + robotsOriginAlias + "/"
}
