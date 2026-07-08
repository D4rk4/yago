//go:build e2e

package containerurl

import (
	"context"
	"net"
	"testing"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
)

func HostURL(t *testing.T, ctx context.Context, container testcontainers.Container, port string) string {
	t.Helper()
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve container host: %v", err)
	}
	mappedPort, err := container.MappedPort(ctx, nat.Port(port))
	if err != nil {
		t.Fatalf("resolve mapped port: %v", err)
	}
	return "http://" + net.JoinHostPort(host, mappedPort.Port())
}
