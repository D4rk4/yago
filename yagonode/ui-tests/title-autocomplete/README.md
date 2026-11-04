# Title and autocomplete browser tests

These Playwright scenarios verify that an imported YaCy title array and its
matching description render as plain text on the local YaCy results page, that
the main portal keeps its visible result window readable, and that autocomplete
returns plain string suggestions with working keyboard and pointer selection.

Run against a local YagoSeek node with the existing corpus:

```sh
npm ci
npm test
```

Set `YAGO_UI_BASE_URL` when the node is not listening at
`http://127.0.0.1:8080`. The title-array scenario uses the indexed
`Compassionate Love | Encyclopedia.com` result through the local-only YaCy
results endpoint, which avoids depending on which shards a progressive portal
search samples. Set `YAGO_TITLE_ARRAY_QUERY` to another query returning that
document when needed. Set `YAGO_AUTOCOMPLETE_QUERY` to select another prefix
with local suggestions. The runner uses Chromium and Firefox and writes
screenshots to the ignored `test-results/` directory.
