// Package e2e runs the end-to-end result-link-router test against containers.
//
// It starts a NATS JetStream broker, the yacyvisitcrawl service, and the real
// searxng/searxng image with the plugin and a self-contained test engine
// mounted in, then drives a search from the host: it checks that the
// returned result link routes through yacyvisitcrawl, that following that
// link redirects to the original destination, and that doing so places a
// crawl order on NATS.
//
// The test is guarded by the e2e build tag and needs a working Docker
// daemon. Run it with `make e2e-plugin`. It is not part of the `make verify`
// quality gate.
package e2e
