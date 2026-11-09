#!/bin/sh
set -eu

out=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"

title='Lantern Harbor Field Notes'
harbor='Harbor lanterns guide the midnight ferry across the quiet bay.'
kettles='Seventeen copper kettles whistle in the orchard kitchen.'
ligatures='The official affidavit flourished after five difficult reflections.'
cyrillic='Съешь же ещё этих мягких французских булок, да выпей чаю.'
cjk='東京の図書館で静かに本を読む。'
owls='Granite owls observe the winter observatory schedule.'
invoice='Invoice 2026-11-08 totals 1,234.56 EUR (net).'

cat > latin.tex <<EOF
\\documentclass{article}
\\usepackage[T1]{fontenc}
\\usepackage{lmodern}
\\usepackage[pdftitle={$title}]{hyperref}
\\begin{document}
$harbor

$kettles

$ligatures
\\newpage
$owls

$invoice
\\end{document}
EOF
pdflatex -interaction=batchmode latin.tex >/dev/null
cp latin.pdf "$out/pdftex-object-streams.pdf"
sed 's/^\\documentclass{article}$/\\pdfobjcompresslevel=0 \\pdfminorversion=4 \\documentclass{article}/' \
	latin.tex > classic.tex
pdflatex -interaction=batchmode classic.tex >/dev/null
qpdf --qdf --object-streams=disable classic.pdf "$out/qpdf-qdf.pdf"

cat > unicode.tex <<EOF
\\documentclass{article}
\\usepackage{luaotfload}
\\font\\body={file:DejaVuSans.ttf}
\\usepackage[pdftitle={$title}]{hyperref}
\\begin{document}
\\body
$harbor

$ligatures

$cyrillic
\\newpage
\\body
$owls
\\end{document}
EOF
lualatex -interaction=batchmode unicode.tex >/dev/null
cp unicode.pdf "$out/luatex-opentype.pdf"

cat > page.html <<EOF
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>$title</title>
<style>body{font-family:"DejaVu Sans","Droid Sans Fallback";font-size:14pt}
.cjk{font-family:"Droid Sans Fallback"} .next{break-before:page}</style></head>
<body><p>$harbor</p><p>$kettles</p><p>$ligatures</p><p>$cyrillic</p>
<p class="cjk">$cjk</p><p class="next">$owls</p><p>$invoice</p></body></html>
EOF
chromium --headless --disable-gpu --no-sandbox --no-pdf-header-footer \
	--print-to-pdf="$work/chromium.pdf" "file://$work/page.html" >/dev/null 2>&1
cp chromium.pdf "$out/chromium-skia.pdf"
pdftocairo -pdf chromium.pdf "$out/poppler-cairo-resaved.pdf"

soffice --headless --convert-to pdf:writer_pdf_Export page.html >/dev/null 2>&1
qpdf --object-streams=generate --linearize page.pdf \
	"$out/qpdf-libreoffice-object-streams-linearized.pdf"
soffice --headless --convert-to \
	'pdf:writer_pdf_Export:{"SelectPdfVersion":{"type":"long","value":"2"},"UseTaggedPDF":{"type":"boolean","value":"true"}}' \
	--outdir pdfa page.html >/dev/null 2>&1
cp pdfa/page.pdf "$out/libreoffice-pdfa2b-tagged.pdf"

cat > page.ps <<EOF
%!PS-Adobe-3.0
[ /Title ($title) /DOCINFO pdfmark
/Times-Roman findfont 14 scalefont setfont
72 720 moveto ($harbor) show
72 700 moveto ($kettles) show
/Helvetica findfont 12 scalefont setfont
72 680 moveto ($ligatures) show
showpage
/Courier findfont 12 scalefont setfont
72 720 moveto ($owls) show
72 700 moveto ($invoice) show
showpage
EOF
gs -q -dBATCH -dNOPAUSE -dSAFER -sDEVICE=pdfwrite -o "$out/ghostscript-pdfwrite.pdf" page.ps

cat > page.ms <<EOF
.TL
$title
.PP
$harbor
.PP
$kettles
.bp
.PP
$owls
EOF
groff -Tpdf -ms page.ms > "$out/groff-gropdf.pdf"

cat > reportlab_page.py <<EOF
from reportlab.pdfgen import canvas
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
pdfmetrics.registerFont(TTFont("DejaVuSans", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"))
page = canvas.Canvas("reportlab.pdf")
page.setTitle("$title")
page.setFont("Helvetica", 12)
page.drawString(72, 720, "$harbor")
page.drawString(72, 700, "$kettles")
page.setFont("DejaVuSans", 12)
page.drawString(72, 680, "$cyrillic")
page.showPage()
page.setFont("Times-Roman", 12)
page.drawString(72, 720, "$owls")
page.drawString(72, 700, "$invoice")
page.save()
EOF
python3 reportlab_page.py
cp reportlab.pdf "$out/reportlab-canvas.pdf"

magick -size 1700x500 xc:white -font DejaVu-Sans -pointsize 48 -fill black \
	-annotate +60+120 "$harbor" -annotate +60+220 "$kettles" -annotate +60+320 "$owls" \
	-density 300 -units PixelsPerInch scan.png
tesseract scan.png tesseract -l eng pdf >/dev/null 2>&1
cp tesseract.pdf "$out/tesseract-ocr-layer.pdf"
