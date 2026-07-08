// Package e2e runs the end-to-end renderproxy test against containers.
//
// It starts a scripted static origin, a lightpanda CDP browser, and the
// renderproxy image on a hermetic Docker network, then drives renderproxy
// from the host as a forward proxy: it issues an absolute-URL GET for the
// origin and asserts the returned body carries the text the origin's script
// writes at render time. A second case sends an HTTP CONNECT and asserts it
// is refused before any origin is reached.
//
// The test is guarded by the e2e build tag and needs a working Docker daemon.
// Run it with `make e2e`. It is not part of the `make verify` quality gate.
package e2e
