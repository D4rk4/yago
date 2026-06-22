// Package e2e runs end-to-end interoperability tests that drive a real YaCy
// server against the node under test over a hermetic Docker network.
//
// The tests are guarded by the e2e build tag and require a Docker daemon. Run
// them with `make e2e`. They are not part of the `make verify` quality gate.
package e2e
