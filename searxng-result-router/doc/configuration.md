# searxng-result-router configuration

The plugin is configured through an environment variable in the SearXNG process, and enabled
through SearXNG's own `settings.yml`.

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `YACYVISITCRAWL_BASE_URL` | required | Base URL of the `yacyvisitcrawl` that rewritten result links route through, e.g. `http://yacyvisitcrawl:8091`. |

## Enabling the plugin

Add the plugin's directory to SearXNG's Python path, then add it to `settings.yml`:

```yaml
plugins:
  result_link_router.SXNGPlugin:
    active: true
```
