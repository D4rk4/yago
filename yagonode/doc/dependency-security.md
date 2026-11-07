# Dependency security

The node and crawler pin gRPC v1.83.2. This includes the receive-memory correction for fragmented HTTP/2
streams described in [the upstream gRPC advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-vp52-pcj8-j9qc).
It also includes the missing-authority-header correction associated with an xDS
routing panic in [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443).

The node and isolated end-to-end test graphs pin x/crypto revision v0.58.1-0.20261009181718-2f1f834330b2, which
includes the authentication restriction correction described in [GO-2026-6303](https://pkg.go.dev/vuln/GO-2026-6303)
and removes the unmaintained OpenPGP packages described in [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932).

The node, crawler, crawl contract, and isolated end-to-end test graphs pin x/net v0.62.0. This includes the HTML
text-node coalescing correction described in [GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860) and the raw-text
namespace rendering correction described in [GO-2026-6866](https://pkg.go.dev/vuln/GO-2026-6866).

The node, crawler, and isolated end-to-end test graphs pin klauspost/compress v1.20.2, which includes the dictionary
integer-overflow correction described in [GO-2026-5841](https://pkg.go.dev/vuln/GO-2026-5841) and the two-block
frame panic correction described in [GO-2026-6865](https://pkg.go.dev/vuln/GO-2026-6865). Values the node stored
with the earlier release remain readable, and the earlier release reads values written with this one.

The crawler container pins its existing `libblkid` and `libmount` dependencies
to Alpine `2.42.3-r1`, addressing the util-linux advisories reported against their
earlier package revision. It also pins the existing libexpat library to
`2.10.0-r0`, which corrects the NULL pointer dereference reported as
CVE-2026-117610 and CVE-2026-118370 against `2.9.0-r0`, and refreshes the CA certificates package to `20261020-r0`.
The crawler image pins PCRE2 to `10.49-r0`, addressing CVE-2026-103111 in
its earlier runtime package. [PCRE2 10.49](https://github.com/PCRE2Project/pcre2/releases/tag/pcre2-10.49)
corrects an out-of-bounds write under specific JIT stack conditions; no exploit
in this project's deployment was established. The existing OpenSSL runtime pins
are `3.5.9-r0`, available in the same Alpine release on both architectures.
The crawler image pins musl and musl-utils to `1.2.6-r3`, which corrects the
regcomp integer overflow reported as CVE-2026-107891 against `1.2.6-r2`, and
zlib to `1.4-r0`, which corrects the gz_vacate heap buffer overflow reported as
CVE-2026-85091 against `1.3.2-r0`.

Bare-metal installations receive host-library fixes
through their operating system's security updates.

These pins describe the current source tree. A release must contain these pins
to carry the corrections; upgrade both binaries from the same verified release.
Runtime authentication, crawl authorization, and YaCy
wire contracts continue to apply. Source and container scans reject blocking
dependency advisories; the normal verification gate also checks reachable Go
vulnerabilities and node-crawler transport behavior.
