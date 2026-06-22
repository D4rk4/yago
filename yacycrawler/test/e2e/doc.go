// Package e2e runs the end-to-end crawler test that drives the service with a
// real headless browser fetcher against a live origin.
//
// The test is guarded by the e2e build tag and requires a working Chromium
// installation. Run it with `make e2e`. It is not part of the `make verify`
// quality gate.
package e2e
