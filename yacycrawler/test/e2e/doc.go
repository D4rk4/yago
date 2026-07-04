// Package e2e runs the end-to-end crawler test against containers.
//
// It starts a NATS JetStream broker, a static origin site, and the crawler
// image on a hermetic Docker network, then drives the crawler over NATS from
// the host: it publishes a crawl order and reads back the crawled page index
// the crawler produces from fetching the origin.
//
// The test is guarded by the e2e build tag and needs a working Docker daemon.
// Run it with `make e2e`. It is not part of the `make verify` quality gate.
package e2e
