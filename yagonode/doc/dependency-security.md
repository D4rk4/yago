# Dependency security

The node and crawler pin gRPC v1.83.1. This includes the receive-memory correction for fragmented HTTP/2
streams described in [the upstream gRPC advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-vp52-pcj8-j9qc).

The node and isolated end-to-end test graphs pin x/crypto v0.55.0, which includes the authentication restriction correction
described in [GO-2026-6303](https://pkg.go.dev/vuln/GO-2026-6303).

The crawler container pins its existing `libblkid` and `libmount` dependencies
to Alpine `2.42.3-r1`, addressing the util-linux advisories reported against their
earlier package revision. Bare-metal installations receive host-library fixes
through their operating system's security updates.

These pins describe the current source tree. A release must contain these pins
to carry the corrections; upgrade both binaries from the same verified release.
Runtime authentication, crawl authorization, and YaCy
wire contracts continue to apply. Source and container scans reject blocking
dependency advisories; the normal verification gate also checks reachable Go
vulnerabilities and node-crawler transport behavior.
