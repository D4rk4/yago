#!/bin/sh
set -eu

out=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"

title='Lantern Harbor Field Notes'
owls='Granite owls observe the winter observatory schedule.'
kettles='Seventeen copper kettles whistle in the orchard kitchen.'
owner='owner-secret'
reader='reader-secret'

cat > page.txt <<EOF
%%MediaBox 0 0 595 842
%%Font Helv Helvetica
BT /Helv 12 Tf 72 770 Td ($owls) Tj 0 -20 Td ($kettles) Tj ET
EOF
mutool create -o page.pdf page.txt
cat > title.js <<EOF
var document = Document.openDocument("page.pdf");
document.setMetaData("info:Title", "$title");
document.save("base.pdf", "compress,garbage");
EOF
mutool run title.js

weak() {
	qpdf --allow-weak-crypto --static-id --static-aes-iv "$@"
}
weak --encrypt --user-password= --owner-password="$owner" --bits=40 -- \
	base.pdf "$out/qpdf-rc4-40-r2.pdf"
weak --encrypt --user-password= --owner-password="$owner" --bits=128 --use-aes=n -- \
	base.pdf "$out/qpdf-rc4-128-r3.pdf"
weak --encrypt --user-password= --owner-password="$owner" --bits=128 --use-aes=y -- \
	base.pdf "$out/qpdf-aes-128-r4.pdf"
weak --encrypt --user-password= --owner-password="$owner" --bits=128 --use-aes=y \
	--cleartext-metadata -- base.pdf "$out/qpdf-aes-128-r4-cleartext-metadata.pdf"
weak --object-streams=generate --encrypt --user-password= --owner-password="$owner" \
	--bits=128 --use-aes=y -- base.pdf "$out/qpdf-aes-128-r4-object-streams.pdf"
weak --encrypt --user-password= --owner-password="$owner" --bits=256 --force-R5 -- \
	base.pdf "$out/qpdf-aes-256-r5.pdf"
weak --encrypt --user-password= --owner-password="$owner" --bits=256 -- \
	base.pdf "$out/qpdf-aes-256-r6.pdf"
weak --object-streams=generate --encrypt --user-password= --owner-password="$owner" \
	--bits=256 -- base.pdf "$out/qpdf-aes-256-r6-object-streams.pdf"
weak --encrypt --user-password="$reader" --owner-password="$owner" --bits=256 -- \
	base.pdf "$out/qpdf-aes-256-r6-user-password.pdf"
weak --encrypt --user-password="$reader" --owner-password="$owner" --bits=128 --use-aes=n -- \
	base.pdf "$out/qpdf-rc4-128-r3-user-password.pdf"
weak --encrypt --user-password= --owner-password="$owner" --bits=128 --use-aes=y \
	--extract=n -- base.pdf "$out/qpdf-aes-128-r4-extract-forbidden.pdf"

cat > page.ps <<EOF
%!PS-Adobe-3.0
[ /Title ($title) /DOCINFO pdfmark
/Helvetica findfont 12 scalefont setfont
72 770 moveto ($owls) show
72 750 moveto ($kettles) show
showpage
EOF
ghostscript() {
	gs -q -dBATCH -dNOPAUSE -dSAFER -sDEVICE=pdfwrite -sOwnerPassword="$owner" "$@"
}
ghostscript -dEncryptionR=2 -dKeyLength=40 -dPermissions=-4 \
	-o "$out/ghostscript-rc4-40-r2.pdf" page.ps
ghostscript -dEncryptionR=3 -dKeyLength=128 -dPermissions=-4 \
	-o "$out/ghostscript-rc4-128-r3.pdf" page.ps

cat > page.html <<EOF
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>$title</title></head>
<body><p>$owls</p><p>$kettles</p></body></html>
EOF
soffice --headless --convert-to \
	'pdf:writer_pdf_Export:{"RestrictPermissions":{"type":"boolean","value":"true"},"PermissionPassword":{"type":"string","value":"'"$owner"'"},"EnableCopyingOfContent":{"type":"boolean","value":"true"}}' \
	page.html >/dev/null 2>&1
cp page.pdf "$out/libreoffice-owner-password.pdf"
