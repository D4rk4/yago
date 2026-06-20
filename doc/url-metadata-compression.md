# URL metadata compression

URL metadata rows are stored in the `urls` bucket as property-form text. Each row repeats
the same field scaffolding (`{author=…,descr=…,flags=…,…}` in sorted key order), so the
bucket holds large amounts of redundant structure. The node compresses each row with DEFLATE
(`compress/flate`) seeded by a preset dictionary, halving the on-disk footprint with no new
dependency.

A version sentinel byte (`0x01`) prefixes every compressed value. Values that do not start
with the sentinel are read as legacy plaintext, so reads keep working before a migration and
the `yacy-url-migrate` command rewrites old rows in place.

## Dictionary

The dictionary is a frozen, checked-in artifact (`yacymodel/url_metadata_dictionary.bin`,
~32 KB, the DEFLATE window limit). It is the verbatim concatenation of whole sample rows taken
in order until the window fills. Because every row shares the same field layout, a whole sample
row lets the compressor match a long continuous run spanning many fields at once.

## Benchmark

Measured per record (the access pattern is random point lookups, so each row is compressed
independently) on a 443,094-row production corpus. The dictionary was trained on a deterministic
subset; the ratios below are over the held-out remainder (388k rows, 230 MB raw).

| Codec | Ratio | Notes |
| --- | --- | --- |
| plaintext | 1.00x | baseline |
| flate, no dictionary | 1.40x | stdlib, no dependency |
| flate + frequency-built dictionary | 2.06x | n-gram trainer, pure Go |
| flate + whole-row dictionary | **2.33x** | chosen |
| zstd, no dictionary | 1.36x | adds `klauspost/compress` |
| zstd + trained dictionary | 2.26x | per-record dictionary re-seeding is slow |

Findings that drove the choice:

- **zstd loses.** Despite being a new dependency, zstd with a trained dictionary (2.26x) does
  not beat flate with a whole-row dictionary (2.33x). zstd's stateless per-call encoder re-seeds
  the entire dictionary before each short record, which is both slower and no smaller here, so
  the dependency was dropped.
- **A hand-built dictionary loses to naive sampling.** An n-gram frequency trainer (2.06x)
  underperforms plain whole-row concatenation (2.33x). The records are so uniform that shredding
  the corpus into frequency-ranked substrings destroys the long-range field adjacency the
  compressor exploits. For this data the simple dictionary is the better dictionary.
