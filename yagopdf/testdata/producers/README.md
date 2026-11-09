# Producer regression fixtures

Each PDF in this directory was written by a real PDF producer from synthetic
input. `generate.sh` holds the complete input: one invented title and invented
English, Russian, and Japanese sentences. No fixture contains third-party
documents, operator data, or crawled content.

The tests read these files directly. No producer program runs during tests.

## Fixtures

| Fixture | Producer | What it exercises |
| --- | --- | --- |
| `chromium-skia.pdf` | Chromium 148.0.7778.178 (Skia/PDF m148) | per-glyph positioning, ligature glyphs, Cyrillic, CJK, tagged PDF |
| `poppler-cairo-resaved.pdf` | Poppler `pdftocairo` 25.03.0 with cairo 1.18.4 | one font split into several subsets, ligatures, punctuation, no title |
| `pdftex-object-streams.pdf` | pdfTeX 1.40.29 (TeX Live 2026) | object streams, cross-reference stream, Type 1 Latin Modern |
| `qpdf-qdf.pdf` | pdfTeX 1.40.29, rewritten by qpdf 12.4.2 in QDF mode | uncompressed streams, classic cross-reference table |
| `luatex-opentype.pdf` | LuaHBTeX 1.24.0 (TeX Live 2026) | OpenType Identity-H fonts, Cyrillic, object streams |
| `libreoffice-pdfa2b-tagged.pdf` | LibreOffice 26.2.3.2 | PDF/A-2b, tagged structure, UTF-16 title |
| `qpdf-libreoffice-object-streams-linearized.pdf` | LibreOffice 26.2.3.2, rewritten by qpdf 12.4.2 | linearized file with object streams |
| `ghostscript-pdfwrite.pdf` | GPL Ghostscript 10.08.0 `pdfwrite` | PostScript source, standard fonts not embedded, object streams |
| `groff-gropdf.pdf` | GNU groff 1.24.2 `gropdf` | standard fonts without Unicode maps, no title |
| `reportlab-canvas.pdf` | ReportLab 5.0.0 | PDF 1.3, TrueType subset, standard fonts |
| `tesseract-ocr-layer.pdf` | Tesseract 5.5.0 on an ImageMagick 7.1.2-31 image | invisible OCR text over a scanned image, no title |

Embedded fonts are subsets of DejaVu, Droid Sans Fallback, and Latin Modern,
which are free fonts shipped by Debian.

## Reproducing

The fixtures were generated on 2026-11-08 with the Debian packages of the
versions listed above. With those programs installed, run:

```sh
sh generate.sh
```

The script overwrites the fixtures in this directory. New files differ in
timestamps and document identifiers but carry the same text.
