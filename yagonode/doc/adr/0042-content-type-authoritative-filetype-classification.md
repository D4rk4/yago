# 0042. Classify a document's file type from its Content-Type, not only its URL

Date: 2026-07-10

## Status

Accepted

## Context

A crawl of arxiv.org returned nothing for `filetype:pdf`, and the filetype
navigation facet listed garbage tokens like `10294` and `15654`. The crawler
was not at fault: the HTTP fetch path admits every content type
(`page_fetcher.go`), the format registry has a working PDF extractor
(`formatparse/pdf.go`, dispatched by MIME as well as extension), the PDF toggle
is on by default (`DefaultFormatToggles`), and the node's ingest gate exempts
non-web content. The PDFs were fetched, parsed, and indexed.

The defect is on the search side, and it is not specific to PDF. A document's
file type — for both the `filetype:` operator and the filetype facet — was
derived **solely from the URL path extension** (`path.Ext`). arxiv serves PDFs
at extension-less URLs (`/pdf/2401.12345`), so the extension is empty or a bare
id fragment (`.12345`); the same is true of any CMS-style URL (`/about`,
`/pdf/gr-qc/9903045`). Every crawled format is affected: an HTML page at `/about`
has no `html` extension either, which is why `filetype:html` also returned
nothing. The stored `documentstore.Document.ContentType` — the authoritative
statement of what the document is — was ignored.

Investigation surfaced four related search-result defects and one crawler-side
gap, all rooted in the same "the URL extension is the file type" assumption:

1. **Classification ignores Content-Type.** `filetype:X` and the facet miss
   every document served at an extension-less URL, for all formats.
2. **Facets are counted before the outer filters.** `FileType`/`SiteHost`/
   `InURL`/`TLD` are applied a layer above the index (`searchlocal`,
   `documentsearch`), after `searchindex` already counted the navigation facets,
   so the host facet shows 63 while the filtered query returns 1.
3. **Modifier-only queries return nothing.** A bare `filetype:pdf` (or `site:`,
   `tld:`, `inurl:`) has no query terms, so no postings are retrieved and the
   filter has nothing to filter.
4. **The crawler's parser dispatch is extension-driven.** `parseOffice`,
   `parseMisc`, and the rtf/msg text branch switch on the URL extension, so an
   office or vCard document at an extension-less URL falls through unparsed; the
   office family's MIME set also omits the real OOXML types.
5. **Local results carried no size.** The RWI and peer result paths report a
   document size; the bleve local path left `searchcore.Result.Size` unset.

## Decision

Make a document's **Content-Type authoritative** for its file type, with the URL
extension as the fallback, and fix the four related defects. The work lands as a
staged epic; each slice is independently `make verify`-green.

1. **`filetypeclass` package (SEARCH-FILETYPE-01, this ADR's first slice).** A
   node-level classifier maps a Content-Type to the canonical file-type token
   (`application/pdf` → `pdf`, covering the crawler's `formatparse` families plus
   common media/application types), with URL-extension fallback and alias folding
   (`jpg`/`jpeg`, `htm`/`html`). `Canonical(url, ct)` gives the facet its display
   token; `Matches(url, ct, wanted)` answers a `filetype:` query when the wanted
   token matches the content-type token **or** the URL extension — a superset of
   the old behavior, so a `foo.pdf` URL still answers `filetype:pdf` and an
   extension-less `application/pdf` document now does too. Applied at the facet
   (`searchindex/facets.go`) and both local filter paths (`searchlocal`,
   `documentsearch`), carrying `ContentType` (and the missing `Size`,
   SEARCH-FILETYPE-04) onto `searchcore.Result`. Classification is query-time, so
   the existing index benefits without a reindex.

2. **Facet/filter consistency (SEARCH-FILETYPE-02).** Move `FileType`/`SiteHost`/
   `InURL`/`TLD` filtering into `searchindex` alongside the other post-retrieval
   filters, so the navigation counts are taken over the same set the results come
   from.

3. **Modifier-only browse queries (SEARCH-FILETYPE-03).** A bare `filetype:pdf`
   (or `site:`/`tld:`/`inurl:` with no keyword) stays keyword-seeded, matching
   YaCy: retrieval walks the postings by word hash, so a match-all
   retrieve-then-filter over the whole index would be a full-index scan on every
   bare-operator query — a denial-of-service lever on the public node — for no
   wire-compatibility gain. The surfaces keep the YaCy behavior and, when a
   filter-only query returns nothing, show a one-line hint (the `modifierhint`
   package) telling the searcher to add a word; facet navigation already lets a
   searcher browse by type after any keyword query (SEARCH-FILETYPE-02).

4. **Content-type/magic parser dispatch (CRAWL-FORMAT-01) and per-format live
   verification (CRAWL-FORMAT-02).** Dispatch the format families on the routing
   Content-Type and magic bytes, not only the URL extension; add the real OOXML
   MIME types; and verify each supported format end-to-end with a real sample
   file crawled through the node.

## Consequences

- `filetype:X` and the filetype facet recognise a document by what it is. arxiv
  PDFs (and every extension-less document) answer `filetype:pdf`; the facet shows
  `pdf`/`html` instead of `10294`. No reindex is needed — the classification runs
  over the already-stored `ContentType`.
- The YaCy wire format is unchanged: the `filetype:` operator, the `filetype`
  navigator name, and the response fields are the same. The match is a superset
  of YaCy's URL-extension semantics (still matches a `.pdf` URL, additionally
  matches by content type), which is a compatible enrichment, not a break.
- Local results now report a size like peer results, populated from the indexed
  text length (matching the RWI path's `size` convention).
- A bare filter query (`filetype:pdf` with no keyword) still returns nothing, as
  in YaCy — a match-all-then-filter retrieve would scan the whole index on every
  such query, a denial-of-service lever on the public node, for no wire gain. The
  portal and admin surfaces instead explain the empty result with a one-line hint
  to add a keyword (`modifierhint`); the facet sidebar stays the zero-friction way
  to narrow a keyword query by type.
- One classifier owns the content-type↔extension mapping, so the crawler's parse
  dispatch and the search-side classification cannot drift apart per format.
