// Package e2e runs the end-to-end full-text indexing test against containers.
//
// It starts NATS JetStream, a static origin page, an egress proxy, Elasticsearch,
// yacynode, yacycrawler (with its crawled-page sink enabled), and yacytextindexer
// on a hermetic Docker network, then drives the crawler over NATS from the host and
// polls Elasticsearch's own search API for the indexed page.
//
// The test is guarded by the e2e build tag and needs a working Docker daemon.
// Run it with `make e2e`. It is not part of the `make verify` quality gate.
package e2e
