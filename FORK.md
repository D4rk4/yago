# yago Fork Notice

`yago` is an independent fork of `yacy-rwi-node` by Nikita Karpei
(https://github.com/nikitakarpei/yacy-rwi-node), a YaCy-compatible peer. This
document states what the fork aims to be, what it claims about compatibility, and
the legal notices it must carry.

## Fork Goals

- Grow a single self-hostable Go search appliance: a YaCy-compatible
  peer-to-peer node, an optional crawler, local and federated search, a
  Tavily-compatible Search API, and an administration UI.
- Keep the YaCy wire protocol and observable peer behavior stable so the node
  interoperates with Java YaCy peers on the same network.
- Build a modern Go search, crawler, and P2P stack behind explicit component
  boundaries instead of porting Java YaCy, Solr, Lucene, or Kelondro internals.
- Never require a JVM, Solr, Lucene, or Kelondro runtime, and never require any
  external or paid search service.

The user-facing roadmap is in
[yagonode/doc/fork-roadmap.md](yagonode/doc/fork-roadmap.md). The full engineering
plan is in [PLAN.md](PLAN.md).

## Compatibility Claims

- This fork does not claim full Java YaCy Search Server compatibility.
  Compatibility is implemented and verified one surface at a time.
- Supported, partial, planned, and unsupported surfaces are listed in
  [yagonode/doc/compatibility.md](yagonode/doc/compatibility.md), also served at
  `GET /api/admin/v1/compatibility`.
- The `POST /search` and `POST /extract` endpoints are a Tavily-compatible API
  surface served from this node's own search core. They are not the Tavily service
  and never call it; there is no outbound upstream Tavily provider (see
  [ADR-0019](yagonode/doc/adr/0019-ddgs-web-search-fallback.md)). `POST /search` is
  a drop-in Tavily Search API: it returns only Tavily-shaped fields, carries no
  yago-specific provenance markers, and is search-only. The one optional external
  augmentation is the admin-toggled DDGS web-search fallback, off by default; its
  `[ddgs]` marker appears only on the human search surfaces (the public search
  portal, the admin search UI, and `/yacysearch.*`), never on the Tavily drop-in.
- Java YaCy administration pages (`/*_p.html`) are not cloned. The Go
  administration API and UI are the intended replacement.

## Legal Notices

`yago` is licensed under the GNU Affero General Public License, version 3
(AGPL-3.0). The full text is in [LICENSE](LICENSE). The AGPL copyright and license
notices from the upstream projects are preserved and must remain intact in
redistributed copies and derivative works.

The AGPL covers use over a network, so any user-facing interface for this
software — including the administration UI and any hosted deployment — must offer
its users access to the corresponding source of the running version. The
administration UI therefore carries a visible notice that:

- names the software and its AGPL-3.0 license;
- links to the corresponding source of the running version;
- preserves the upstream attribution to YaCy and to `yacy-rwi-node`.

These notice requirements apply to every build of the administration UI, and new
UI work includes them.
