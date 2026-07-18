//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	restartOriginAlias      = "restart-origin"
	restartSeedPath         = "/seed-restart.html"
	restartPendingPath      = "/pending-restart.html"
	restartPendingStartPath = "/pending-restart-start"
	restartOriginReadyPath  = "/restart-ready.html"
	restartManifestHoldPath = "/manifest-hold.html"
)

type restartOrigin struct {
	seedURL     string
	manifestURL string
	container   testcontainers.Container
}

func startRestartOrigin(t *testing.T, ctx context.Context, networkName string) restartOrigin {
	t.Helper()
	seedPage := fmt.Sprintf(
		`<!doctype html><html><head><title>Committed restart seed</title></head><body><a href=%q>pending durable frontier page</a><p>%s</p></body></html>`,
		restartPendingPath,
		restartIndexableText(80),
	)
	pendingPage := fmt.Sprintf(
		`<!doctype html><html><head><title>Recovered pending page</title></head><body><p>%s</p></body></html>`,
		restartIndexableText(4800),
	)
	manifestPage := fmt.Sprintf(
		`<!doctype html><html><head><title>Recovery manifest hold</title></head><body><p>%s</p></body></html>`,
		restartIndexableText(4800),
	)
	nginxConfiguration := `events {}
http {
  log_format current_uri '$remote_addr - - [$time_local] "$request_method $uri $server_protocol" $status $body_bytes_sent "$http_referer" "$http_user_agent"';
  access_log /dev/stdout current_uri;
  error_log /dev/stderr notice;
  log_subrequest on;
  server {
    listen 80;
    root /usr/share/nginx/html;
    default_type text/html;
    location = /pending-restart.html {
      auth_request /pending-restart-start;
      limit_rate 4096;
    }
    location = /pending-restart-start {
      internal;
      return 204;
    }
    location = /manifest-hold.html {
      limit_rate 1;
    }
  }
}`
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          originImage,
			ExposedPorts:   []string{"80/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {restartOriginAlias}},
			Files: []testcontainers.ContainerFile{
				{
					Reader:            strings.NewReader(nginxConfiguration),
					ContainerFilePath: "/etc/nginx/nginx.conf",
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader(seedPage),
					ContainerFilePath: "/usr/share/nginx/html" + restartSeedPath,
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader(pendingPage),
					ContainerFilePath: "/usr/share/nginx/html" + restartPendingPath,
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader("ready"),
					ContainerFilePath: "/usr/share/nginx/html" + restartOriginReadyPath,
					FileMode:          0o644,
				},
				{
					Reader:            strings.NewReader(manifestPage),
					ContainerFilePath: "/usr/share/nginx/html" + restartManifestHoldPath,
					FileMode:          0o644,
				},
			},
			WaitingFor: wait.ForHTTP(restartOriginReadyPath).WithStartupTimeout(time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("start restart origin container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dumpLogsOnFailure(t, "restart-origin", container)

	return restartOrigin{
		seedURL:     "http://" + restartOriginAlias + restartSeedPath,
		manifestURL: "http://" + restartOriginAlias + restartManifestHoldPath,
		container:   container,
	}
}

func restartIndexableText(wordTotal int) string {
	words := make([]string, 0, wordTotal)
	words = append(words, "the", "and")
	for ordinal := 0; len(words) < wordTotal; ordinal++ {
		words = append(words, restartAlphabeticWord(ordinal))
	}

	return strings.Join(words, " ")
}

func restartAlphabeticWord(ordinal int) string {
	letters := [6]byte{}
	for position := range letters {
		letters[position] = 'a' + byte(ordinal%26)
		ordinal /= 26
	}

	return string(letters[:])
}

func (o restartOrigin) requests(ctx context.Context, path string) int {
	logs, err := o.container.Logs(ctx)
	if err != nil {
		return 0
	}
	defer func() { _ = logs.Close() }()
	raw, err := io.ReadAll(logs)
	if err != nil {
		return 0
	}

	return strings.Count(string(raw), `"GET `+path+` `)
}
