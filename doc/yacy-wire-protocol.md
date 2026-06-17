# YaCy DHT wire protocol

Concise API reference for YaCy peer-to-peer DHT interoperability, based on
upstream `yacy/yacy_search_server` sources and local observations.

All peer-to-peer endpoints live under `/yacy/*.html` and use plain HTTP.
Requests are HTTP form fields, normally `application/x-www-form-urlencoded`;
live DHT transfer may also use gzip-compressed `multipart/form-data`. Responses
are `key=value` lines with CRLF or LF endings. These DHT endpoints do not use
JSON or XML.

---

## 1. Shared primitives

### Enhanced Base64

YaCy hashes, seed strings, and DHT ring positions use the standard Base64 bit
packing with a URL-safe alphabet and no `=` padding:

```text
standard: ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/
YaCy:     ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_
```

The alphabet order is also the DHT collation/ring order.

### Hashes and DHT positions

Word, URL, and peer hashes are fixed-width 12-character enhanced-Base64 values.
YaCy word hashes are:

```text
wordHash = enhancedBase64(MD5(lowercase(word)))[:12]
```

Hashes starting with `_____` are reserved/private. URL hashes are opaque on DHT
receiver paths; YaCy's native URL hash stores domain information in the last 6
bytes.

Horizontal DHT ownership is based on the enhanced-Base64 cardinal of a word
hash:

```text
position(wordHash) = enhancedBase64Cardinal(wordHash)
distance(from,to)  = (to >= from) ? to - from : (MAX - from) + to + 1
```

A peer owns word hashes closest to its peer-hash position. Vertical partitioning
uses leading bits from the URL hash.

### Seeds

A seed string is a comma-separated map of sorted `Key=Value` pairs. Common
fields include:

```text
Hash Name IP IP6 Port PortSSL PeerType Version Uptime UTC LastSeen Flags
```

`PeerType` is one of `virgin`, `junior`, `senior`, or `principal`.

`Flags` is a printable 5-bit atom bitfield. `Seed.FLAGSZERO` is four spaces.
Bits are counted from the least-significant bit of the first atom:

| Bit | Flag | Meaning |
|-----|------|---------|
| 0 | `FLAG_DIRECT_CONNECT` | directly connectable |
| 1 | `FLAG_ACCEPT_REMOTE_CRAWL` | accepts delegated crawls |
| 2 | `FLAG_ACCEPT_REMOTE_INDEX` | accepts DHT index transfer |
| 3 | `FLAG_ROOT_NODE` | root/bootstrap node |
| 4 | `FLAG_SSL_AVAILABLE` | offers HTTPS |

Seed wire forms:

```text
p|<plaintext seed string>
b|<enhanced-base64(seed string)>
z|<enhanced-base64(gzip(seed string))>
```

YaCy chooses the shortest valid representation when generating a seed.

### Network authentication

Every `/yacy/*` DHT servlet checks the YaCy network unit:

```text
network.name=<network unit>
```

Missing `network.name` means `freeworld`. The default uncontrolled `freeworld`
network needs no shared secret. Controlled networks validate:

```text
magicmd5 = MD5hex(key + iam + essentials)
```

---

## 2. Endpoints

### `GET|POST /yacy/hello.html`

Peer handshake and seed exchange.

Request fields:

| Field | Meaning |
|-------|---------|
| `network.name` | YaCy network unit. |
| `key` | Salt for controlled-network response seed encoding. |
| `seed` | Requester's seed in `p|`, `b|`, or `z|` form. |
| `count` | Requested seed count; capped at 100. |
| `iam` | Requester's peer hash. |
| `magicmd5` | Controlled-network authentication value. |
| `mytime` | Requester's current time. |

Response fields:

| Field | Meaning |
|-------|---------|
| `version` | Responder YaCy version. |
| `uptime` | Responder uptime in minutes. |
| `yourip` | IP address observed by the responder. |
| `yourtype` | Requester classification: `virgin`, `junior`, `senior`, or `principal`. |
| `mytime` | Responder time. |
| `message` | Status text, commonly `ok ...` or `bad seed: ...`. |
| `seed0` | Responder's own seed. |
| `seed1..seedN` | Additional known seeds. |

`seed0` is always the responder's own seed.

### `POST /yacy/transferRWI.html`

Transfers reverse-word-index postings.

Request fields:

| Field | Meaning |
|-------|---------|
| `network.name` | YaCy network unit. |
| `iam` | Sender peer hash. |
| `youare` | Receiver peer hash; mismatch returns `wrong_target`. |
| `wordc` | Announced distinct word-hash count. |
| `entryc` | Announced index-entry count. |
| `indexes` | Newline-delimited RWI entries. |
| `key` | Controlled-network key/salt parameter. |

Each non-empty `indexes` line is:

```text
<12-char wordHash>{<WordReferenceRow property form>}
```

The property form is comma-separated `col=value` data. Common columns are:

| Column | Meaning |
|--------|---------|
| `h` | URL hash |
| `a` | last modified |
| `s` | fresh until |
| `u` | title word count |
| `w` | text word count |
| `p` | phrase count |
| `d` | document type |
| `l` | language |
| `x` | local link count |
| `y` | external link count |
| `m` | URL length |
| `n` | URL component count |
| `g` | word type |
| `z` | flags |
| `c` | hit count |
| `t` | text position |
| `r` | phrase-relative position |
| `o` | phrase position |
| `i` | word distance |
| `k` | reserve |

Receiver validity filters: line contains `{`, line contains `x=`, line does not
contain `[B@`, and both word hash and URL hash are 12 characters. Receivers may
drop entries beyond the flood-guard limit of 1000 accepted entries.

Response fields:

| Field | Meaning |
|-------|---------|
| `version` | Responder YaCy version. |
| `uptime` | Responder uptime in minutes. |
| `result` | Common values: `ok`, `busy`, `not_granted`, `wrong_target`, `too high load`. |
| `pause` | Backoff hint in milliseconds. |
| `unknownURL` | Comma-separated URL hashes requested via `transferURL.html`. |

### `POST /yacy/transferURL.html`

Transfers URL metadata rows requested with `unknownURL`.

Request fields:

| Field | Meaning |
|-------|---------|
| `network.name` | YaCy network unit. |
| `iam` | Sender peer hash. |
| `youare` | Receiver peer hash; mismatch returns `wrong_target`. |
| `urlc` | URL metadata row count. |
| `url0..urlN` | Indexed URIMetadataNode property-form rows. |

Each `urlN` value is comma-separated URIMetadataNode property data. The URL hash
is commonly in `hash=`, and some versions/paths use `h=`. YaCy rejects rows
older than 2006-11-01 based on metadata date fields.

Response fields:

| Field | Meaning |
|-------|---------|
| `version` | Responder YaCy version. |
| `uptime` | Responder uptime in minutes. |
| `result` | `ok`, `wrong_target`, or `error_not_granted`. |
| `double` | Count of URLs already known by the receiver. |

### `GET|POST /yacy/search.html`

Remote search over DHT word hashes.

Request fields:

| Field | Meaning |
|-------|---------|
| `network.name` | YaCy network unit. |
| `myseed` | Requester seed. |
| `query` | Concatenated 12-character word hashes, no separator. |
| `exclude` | Exclusion filter. |
| `urls` | URL-hash prefilter. |
| `count` | Requested result count; default 10. |
| `time` | Search budget in milliseconds; commonly clamped around 3000. |
| `maxdist` | Maximum word distance. |
| `partitions` | Partition count; default 30. |
| `abstracts` | Empty, `auto`, or word hashes for abstract generation. |
| `contentdom` | Content-domain filter. |
| `strictContentDom` | Strict content-domain flag. |
| `timezoneOffset` | Requester's timezone offset. |
| `language` | Language filter. |
| `modifier`, `prefer`, `filter`, `constraint`, `profile` | Search expressions/profile. |
| `sitehost`, `sitehash`, `author`, `collection`, `filetype`, `protocol` | Optional modifier fields. |

Response fields:

| Field | Meaning |
|-------|---------|
| `version` | Responder YaCy version. |
| `uptime` | Responder uptime in minutes. |
| `searchtime` | Search time in milliseconds. |
| `references` | Topic/reference terms. |
| `joincount` | Total joined matches. |
| `count` | Number of returned resources. |
| `resource0..resourceN` | URIMetadataNode rows for returned URLs. |
| `indexcount.<wordhash>` | Available index count for a queried word hash. |
| `indexabstract.<wordhash>` | Optional compressed abstract container. |

The returned-resource count key is `count=`, not `linkcount=`. YaCy rate-limits
remote search requests per client IP.

### `GET|POST /yacy/query.html`

Status and capacity query endpoint.

Request fields:

| Field | Meaning |
|-------|---------|
| `network.name` | YaCy network unit. |
| `youare` | Receiver peer hash; mismatch rejects the request. |
| `iam` | Sender peer hash. |
| `object` | Requested object. |
| `env` | Object-specific argument, such as a word hash for `rwiurlcount`. |

Known `object` values:

```text
rwicount rwiurlcount lurlcount wantedlurls wantedpurls wantedword wantedrwi wantedseeds
```

Response fields:

| Field | Meaning |
|-------|---------|
| `version` | Responder YaCy version. |
| `uptime` | Responder uptime in minutes. |
| `response` | Numeric response; `-1` means rejected or wrong target. |
| `mytime` | Responder time. |
| `magic` | Network magic/auth response field. |

### `POST /yacy/crawlReceipt.html`

Acknowledges or rejects delegated crawl work.

Request fields:

| Field | Meaning |
|-------|---------|
| `network.name` | YaCy network unit. |
| `iam` | Sender peer hash. |
| `youare` | Receiver peer hash. |
| `result` | Crawl result reported by the sender. |
| `reason` | Sender-provided result reason. |
| `lurlEntry` | Encoded URL metadata entry for the crawl result. |

Response fields:

| Field | Meaning |
|-------|---------|
| `version` | Responder YaCy version. |
| `uptime` | Responder uptime in minutes. |
| `delay` | Delay hint in seconds before further crawl delegation. |
