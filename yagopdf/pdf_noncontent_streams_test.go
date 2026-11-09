package yagopdf

import "testing"

func TestPDFPagelessNonContentReferencesFollowIndirectContainers(t *testing.T) {
	for _, references := range []string{
		"/OpenAction << /JS 7 0 R >>", "/EF 7 0 R", "/XFA 7 0 R",
		"/Metadata 7 0 R", "/FontFile2 7 0 R",
	} {
		body := newSyntheticPDF().indirectObject(20, "<< "+references+" >>").
			indirectObject(7, "<< /F 8 0 R /Cycle 7 0 R >>").
			rawStream(2, "", "BT (Legitimate fallback words) Tj ET").
			rawStream(8, "", "BT (Excluded payload words) Tj ET").finished()
		document, readable := ReadPDF(body)
		if !readable || document.Text != "Legitimate fallback words" {
			t.Fatalf("indirect exclusion %q: %t %q", references, readable, document.Text)
		}
	}
}

func TestPDFPagelessReferenceLookalikesDoNotHideContent(t *testing.T) {
	for _, entries := range []string{
		"/Note (/JS 8 0 R)", "% /EF << /F 8 0 R >>\n",
		"/JSName 8 0 R", "/XFAName 8 0 R", "/Text <2f4a53203820302052>",
	} {
		body := newSyntheticPDF().indirectObject(20, "<< "+entries+" >>").
			rawStream(8, "", "BT (Legitimate fallback words) Tj ET").finished()
		document, readable := ReadPDF(body)
		if !readable || document.Text != "Legitimate fallback words" {
			t.Fatalf("lookalike %q: %t %q", entries, readable, document.Text)
		}
	}
}
