# Admin-console gap analysis vs YaCy (UI-GAP, 2026-07)

Method: two full inventories — YaCy's protected htroot pages grouped by its
submenu structure, and this node's admin console with its settings catalog —
then a functional diff. YaCy source at commit of 2026-07 (AI-fork of
yacy_search_server; AI Lab pages ignored as fork-specific).

## Closed gaps (implementation order)

| # | YaCy source | Gap | Our slice |
|---|---|---|---|
| UI-15 | ConfigRobotsTxt_p | We served no /robots.txt: foreign spiders could crawl the infinite SERP space. | `publicrobots` package + `web.robots.policy` live setting (no-serp default / open / closed). |
| UI-16 | AccessTracker_p | No admin view of search activity (recent queries, top words, latency) — only slog lines. | "Search activity" admin section fed by a privacy-respecting in-memory ring (off records nothing; aggregate records shapes only; full records query text + top words). |
| UI-17 | BlacklistTest_p, BlacklistImpExp_p | Denylist has no test-a-URL probe and no import/export. | Test field + plaintext export/import in the Index blacklist manager. |
| UI-18 | IndexExport_p | No index export. | Export indexed URLs (text / CSV / JSONL) filtered by domain/query from the Index section. |
| UI-19 | Automation_p (recorded API calls + recurring schedule) | Crawls are one-shot; no recurring re-crawl. | "Crawl schedules": saved crawl profiles re-dispatched on an interval. |
| UI-20 | SearchAccessRate_p | Public rate limits exist (YaCy parity, task 100) but are hard-coded — no admin setting. | `search.rate.*` settings in the catalog. |
| UI-21 | ConfigPortal_p (branding subset) | Portal had no operator branding. | `portal.greeting` setting rendered on the portal. |

## Covered already (no action)

ViewLog_p → Logs (severity/category/text filters); Threaddump_p → pprof
(OPS-08); Crawler_p/CrawlProfileEditor terminate → crawl monitor
pause/resume/cancel/rate; IndexControlURLs_p lookup/delete-URL/delete-domain →
Index browser + delete actions; Blacklist_p core → denylist manager;
CrawlResults → crawl monitor tallies + Index browser; News admin (view) →
Network news panel; Status → Overview; PerformanceMemory/Queues tuning → the
typed settings catalog (storage quota, DHT gates, timeouts, intake caps);
ConfigBasic wizard → /admin/setup wizard; ConfigAccounts (admin password) →
Security; RankingRWI/Solr → ranking-profile JSON API + SEARCH-38 defaults;
ConfigHeuristics OpenSearch federation ≈ web-fallback engine settings;
IndexBrowser_p browse/delete ≈ document browser (per-URL + per-domain
delete); Load_RSS ≈ crawler's native feed parsing (FMT-03).

## Deliberately out of scope (documented decisions)

- **Solr machinery** (IndexFederated_p, IndexSchema_p, RankingSolr_p BF/BQ/FQ,
  IndexExportImportSolr_p, optimize/reboot Solr): the engine explicitly has no
  Solr (ADR); our bleve equivalents are managed through the settings catalog.
- **Community publishing** (Wiki, Blog, Messages, Surftips): social features
  orthogonal to search administration; a modern deployment uses dedicated
  tools. Revisit only on operator demand.
- **Proxy subsystem** (Transparent/URL proxy, ProxyIndexingMonitor_p, cookie
  monitors): the node does not proxy user traffic by design.
- **Index packs** (IndexPackGenerator/Downloader/Manager) and bulk importers
  (MediaWiki/WARC/ZIM/OAI-PMH/JsonList/phpBB3): a separate import/export
  track, not an admin-console gap; OPS-03 covers whole-node backup, UI-18
  covers URL export. Candidates for a future IMPORT wave if asked.
- **ConfigUpdate_p auto-update**: releases ship via apt/.deb and container
  images (OPS-05/06); self-updating binaries fight the package manager.
- **ConfigProperties_p raw key editor**: our catalog is typed and validated
  end-to-end; a raw editor would bypass exactly that safety.
- **Skins/UI languages (ConfigAppearance/Language), search-page field toggles**:
  single accessible Carbon theme is a product decision; localization is a
  separate track.
- **UPnP toggle** (ConfigBasic): NAT traversal is an engine feature, not an
  admin gap; tracked in the networking backlog.
- **RemoteCrawl_p** (accept remote crawl jobs): the earlier disabled-stub
  assessment is superseded. Secure opt-in controlled-network admission,
  authenticated staging, quotas, denylist enforcement, and typed Admin settings
  are implemented; remote crawl remains disabled by default.
- **AI Lab** (LLM/RAG/chat pages): fork-specific, out of scope.
