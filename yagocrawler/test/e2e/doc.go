// Package e2e runs the end-to-end crawler test against containers.
//
// It starts a static origin site and the crawler image on a hermetic Docker
// network, and serves an in-process CrawlExchange gRPC endpoint from the host
// that the crawler dials over host.testcontainers.internal. The test enqueues a
// crawl order on that endpoint and reads back the ingest batch the crawler
// submits after fetching the origin.
//
// The test is guarded by the e2e build tag and needs a working Docker daemon.
// Run it with `make e2e`. It is not part of the `make verify` quality gate.
package e2e
