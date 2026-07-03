package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const crawlerMetricsPath = "/metrics"

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

func startCrawlerMetrics(
	ctx context.Context,
	addr string,
	handler http.Handler,
) (io.Closer, error) {
	if addr == "" {
		return noopCloser{}, nil
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen crawler metrics %q: %w", addr, err)
	}

	return serveCrawlerMetrics(listener, handler), nil
}

func serveCrawlerMetrics(listener net.Listener, handler http.Handler) io.Closer {
	mux := http.NewServeMux()
	mux.Handle(crawlerMetricsPath, handler)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()

	return server
}
