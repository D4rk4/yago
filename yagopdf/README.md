# yagopdf

`yagopdf` reads the embedded text of PDF and PostScript files. The crawler uses
it for every PDF it indexes, and `yago-node` uses it when fetch-on-extract
receives a PDF. Both read the same file the same way. The module uses only the
Go standard library.

`ReadPDF` returns the document title and the page text. It reports a file as
unreadable when the file does not start with `%PDF`, cannot be opened without a
password, forbids text extraction, or yields fewer than eight letters of text.
`ReadPostScript` returns the text shown by a PostScript program; PostScript has
no title.

## What is read

- **Pages.** Text comes from the content streams of the pages in the document's
  current page tree and from the form objects those pages use. Pages removed by
  a later update are not read. Outlines, bookmarks, annotations, links,
  attachments, images, embedded fonts, metadata, and streams that no page uses
  are not read as text. A file without a usable page tree falls back to its other
  streams, still skipping those kinds and any stream that the file names as a
  script, attachment, or form data.
- **Stream data.** When a stream states its `/Length` as a number and that
  length ends at `endstream`, the data ends there, so bytes inside the data
  cannot add or replace objects. Streams may be uncompressed or use Flate, LZW,
  ASCII85, or ASCIIHex filters, alone or in a chain.
- **Compressed objects.** Objects packed into object streams are unpacked
  before reading, so page, font, and title objects stored that way are found.
  Object streams nested inside other object streams are not unpacked.
- **Encrypted documents.** A document protected by the standard security
  handler (revisions 2 to 6, RC4 or AES) is read only when it opens with an
  empty user password and its permissions allow text extraction. A document
  that needs a password, forbids extraction, or uses another security handler
  is reported as unreadable. There is no password guessing or bypass.
- **Text operators.** Only text shown inside a `BT` ... `ET` text block is
  read. Operators are recognized as whole tokens, so `BT`, `ET`, or a show
  operator written inside a string or name has no effect.
- **Fonts.** Each page and form resolves font names through its own resources,
  so the same font name can mean different fonts on different pages. A font's
  `/ToUnicode` map is used first; a simple font without a map entry may use its
  standard or custom `/Encoding` with standard glyph names. A font with no
  trustworthy mapping produces no text rather than guessed characters.
- **Spaces and line breaks.** The reader tracks text position, font size,
  spacing, scaling, and glyph widths. It inserts a space when the gap between
  two pieces of text is wider than the glyphs explain, and a line break when
  text moves to a new line.
- **Characters.** Text is valid UTF-8 in any script. Latin ligatures from
  U+FB00 to U+FB06 are written as their separate letters. Control characters,
  bidirectional formatting marks, and Unicode noncharacters are dropped; tabs
  and line feeds are kept, and other control whitespace becomes a space.
- **Title.** The title comes only from the `/Title` of the document
  information dictionary named by the newest trailer. An outline or bookmark
  title never becomes the document title. Without that dictionary or its
  `/Title`, the title is empty.

## Limits

Every document is read within fixed bounds. Work stops at a bound; it never
grows past it.

| Bound | Value |
| --- | --- |
| File bytes searched for objects | 16 MiB |
| Indirect objects per document | 4,096 |
| Content streams per document | 256 |
| Decoded bytes per stream | 8 MiB |
| Decoded bytes per document, all streams together | 32 MiB |
| Decoded object-stream bytes per document, inside the 32 MiB | 8 MiB |
| Returned text | 1 MiB of UTF-8 |
| Returned title | 4 KiB of UTF-8 |
| Nested graphics states tracked | 64 |

Font maps, encodings, and glyph widths have their own fixed caps and share the
document budget.

## Not supported

- **No OCR.** Only an existing text layer is read. A scanned page without one
  yields no text.
- **Type 3 fonts** and composite fonts that use a predefined CMap other than
  Identity without a `/ToUnicode` map usually yield little or no text.
- **Reading order** follows the order of the content stream. Multi-column
  layouts, tables, vertical writing, and right-to-left text may come out in a
  different order than a reader would see.
- Form field values, annotations, and attachments are not extracted.

## Tests

Tests use only synthetic documents. Hand-built PDFs cover each rule above,
including hostile structure, decompression, and text shapes. Fixtures in
`testdata/producers` and `testdata/encrypted` were written by real PDF
producers from invented text; each directory's `generate.sh` reproduces them.
`FuzzReadPDF` and `FuzzReadPostScript` check that any input returns valid
UTF-8 within the text and title bounds.
