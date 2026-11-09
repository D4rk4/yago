package yagopdf

import (
	"strings"
	"testing"
)

func TestPostScriptRequiresShowOperators(t *testing.T) {
	for _, operator := range []string{
		"show", "ashow", "widthshow", "awidthshow", "kshow", "xshow", "yshow", "xyshow",
	} {
		document, readable := ReadPostScript([]byte("%!PS\n(Legitimate visible words) " + operator))
		if !readable || document.Text != "Legitimate visible words" {
			t.Fatalf("%s = %t %q", operator, readable, document.Text)
		}
	}
	for _, program := range []string{
		"%!PS\n(Hidden literal words) pop show", "%!PS\n(Hidden literal words)",
		"%!PS\n% (Hidden comment words) show\nshowpage",
	} {
		if document, readable := ReadPostScript([]byte(program)); readable || document.Text != "" {
			t.Fatalf("unshown text admitted: %t %q", readable, document.Text)
		}
	}
}

func TestPostScriptTextAndInputAreBounded(t *testing.T) {
	text := newPDFTextCollector(8)
	pdfWritePostScriptText(text, []byte("(visible letters) show (not read) show"))
	if text.String() != "visible " || !text.complete() {
		t.Fatalf("bounded text = %q", text.String())
	}
	body := []byte(
		"%!PS\n" + strings.Repeat(" ", pdfMaxObjScanBytes) + "(outside bound words) show",
	)
	if document, readable := ReadPostScript(body); readable || document.Text != "" {
		t.Fatalf("outside input bound admitted: %t %q", readable, document.Text)
	}
}
