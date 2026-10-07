# Dependency security

The node and crawler pin gRPC v1.83.2. This includes the receive-memory correction for fragmented HTTP/2
streams described in [the upstream gRPC advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-vp52-pcj8-j9qc).
It also includes the missing-authority-header correction associated with an xDS
routing panic in [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443).

The node and isolated end-to-end test graphs pin x/crypto v0.55.0, which includes the authentication restriction correction
described in [GO-2026-6303](https://pkg.go.dev/vuln/GO-2026-6303).

The crawler container pins its existing `libblkid` and `libmount` dependencies
to Alpine `2.42.3-r1`, addressing the util-linux advisories reported against their
earlier package revision. It also pins the existing libexpat library to
`2.8.5-r0`, including the malformed UTF-16 correction in
[CVE-2026-93990](https://github.com/libexpat/libexpat/releases/tag/R_2_8_5),
and refreshes the CA certificates package to `20260909-r0`.
The crawler image pins PCRE2 to `10.49-r0`, addressing CVE-2026-103111 in
its earlier runtime package. [PCRE2 10.49](https://github.com/PCRE2Project/pcre2/releases/tag/pcre2-10.49)
corrects an out-of-bounds write under specific JIT stack conditions; no exploit
in this project's deployment was established. The existing OpenSSL runtime pins
are `3.5.9-r0`, available in the same Alpine release on both architectures.

Bare-metal installations receive host-library fixes
through their operating system's security updates.

These pins describe the current source tree. A release must contain these pins
to carry the corrections; upgrade both binaries from the same verified release.
Runtime authentication, crawl authorization, and YaCy
wire contracts continue to apply. Source and container scans reject blocking
dependency advisories; the normal verification gate also checks reachable Go
vulnerabilities and node-crawler transport behavior.
