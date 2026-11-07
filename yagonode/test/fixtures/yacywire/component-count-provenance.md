# URL component count provenance

This file records the Java source that the crawler's generated RWI posting field
`n` (`ColURLComponentCount`) reproduces, so the interop audit trail can be
reconstructed from the repository alone.

## Java revision

- Repository: Java YaCy `yacy_search_server` master.
- Commit: `eba3f94b4376180de5fbf3812c41e3c65dad4cfb`.
- File and method: `net.yacy.document.parser.urls.MultiProtocolURL.urlComps`.

## Algorithm

`urlComps` splits the URL on the delimiter set ` /()-:_.,?!'"` plus space,
after stripping the scheme prefix through `//` only when the `//` index is
greater than zero, and lowercasing the remaining string. Interior empty
components are kept; trailing empties are dropped. The counter is the number of
resulting components, not path depth and not a `FieldsFunc` split.

## Synthetic inputs and Java-measured counts

The malformed examples are counting fixtures only; they must not become accepted
crawler targets.

| Input | Components |
|---|---:|
| `https://example.com/article` | 3 |
| `https://www.example.org/terms-conditions/` | 5 |
| `https://example.com/a//b` | 5 |
| `http://` | 1 |
| `http:///` | 0 |
| `https://example.com/page?query=hello+world` | 4 |
| `https://example.com:8080/path/a-b_c.d,e` | 9 |
| `http://[::1]:80/path/x//y` | 8 |
| `http://example.com/a-b_c.d,e?f=g!` | 8 |
| `example.com/article` | 3 |
| `www.example.com/page` | 4 |
| `http://www.例子.test/x` | 4 |
| `http://www.例子.test/路径/x` | 5 |
| `https://x.com/中文/path` | 4 |
| `https://e/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa/aaaaaaaaaa` | 9 |
| `""` | 1 |
| `"   "` | 0 |
| `http://a` | 1 |
| `http://example.com/a b c` | 5 |
| `http://example.com/(a)/(b)` | 7 |
| `http://example.com/a'b` | 4 |
| `http://example.com/a"b` | 4 |

## Byte-cardinal encoding

Field `n` is a one-byte cardinal. The raw count wraps to its low byte, and
negative counts clamp to zero:

| Raw count | Field `n` |
|---:|---:|
| 254 | 254 |
| 255 | 255 |
| 256 | 0 |
| 300 | 44 |

The Go assertions for both the component count and the byte cardinal live in
`yago-crawler/internal/pageindex/url_component_count_test.go` and
`yago-crawler/internal/pageindex/posting_cardinals_test.go`.
