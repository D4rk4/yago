# renderproxy configuration

renderproxy is configured entirely through environment variables.

## Proxy

| Variable | Default | Meaning |
|---|---|---|
| `RENDERPROXY_LISTEN_ADDR` | `:8080` | Address the forward HTTP proxy accepts requests on. |

## Browser

| Variable | Default | Meaning |
|---|---|---|
| `RENDERPROXY_CDP_URL` | required | CDP endpoint of the browser that loads pages. |
| `RENDERPROXY_RENDER_CONCURRENCY` | `4` | Concurrent renders; requests past this wait. |

## Limits

| Variable | Default | Meaning |
|---|---|---|
| `RENDERPROXY_REQUEST_DEADLINE` | `30s` | Deadline for a single render, including page settle. |
| `RENDERPROXY_MAX_RESPONSE_BYTES` | `10485760` | Largest rendered page returned; larger fails the request. |

## Operations

| Variable | Default | Meaning |
|---|---|---|
| `RENDERPROXY_OPS_ADDR` | `:9090` | Address serving `/health` and `/metrics`. |
| `LOG_LEVEL` | `INFO` | Log level. |
