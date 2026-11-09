package yagopdf

import (
	"fmt"
	"strings"
	"testing"
)

func TestPDFRootedPageTreeDeterminesMembershipAndOrder(t *testing.T) {
	for _, test := range []struct {
		name string
		kids string
		want string
	}{
		{"normal order", "[1 0 R 3 0 R]", "First page words\nSecond page words"},
		{"reverse order", "[3 0 R 1 0 R]", "Second page words\nFirst page words"},
		{"removed second page", "[1 0 R]", "First page words"},
		{"empty rooted tree", "[]", ""},
		{"cycle and unrelated child", "[10 0 R 8 0 R 1 0 R]", "First page words"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := newSyntheticPDF().
				indirectObject(20, "<< /Type /Catalog /Pages 10 0 R >>").
				indirectObject(10, "<< /Type /Pages /Kids 11 0 R >>").
				indirectObject(11, test.kids).
				indirectObject(8, "<< /Type /Other /Kids [3 0 R] >>").
				page(1, "/Parent 10 0 R /Contents 2 0 R").
				rawStream(2, "", "BT (First page words) Tj ET").
				page(3, "/Parent 10 0 R /Contents 4 0 R").
				rawStream(4, "", "BT (Second page words) Tj ET").
				trailer("<< /Root 20 0 R >>").finished()
			document, readable := ReadPDF(body)
			if document.Text != test.want || readable != (test.want != "") {
				t.Fatalf("page tree text = %t %q, want %q", readable, document.Text, test.want)
			}
		})
	}
}

func TestPDFContentsArrayExpansionPreservesTheDeclaredOrder(t *testing.T) {
	body := newSyntheticPDF().page(1, "/Contents [5 0 R 4 0 R]").
		indirectObject(5, "[2 0 R 3 0 R]").
		rawStream(2, "", "BT (First section words) Tj ET").
		rawStream(3, "", "BT (Second section words) Tj ET").
		rawStream(4, "", "BT (Third section words) Tj ET").finished()
	document, readable := ReadPDF(body)
	if !readable ||
		document.Text != "First section words\nSecond section words\nThird section words" {
		t.Fatalf("contents order = %t %q", readable, document.Text)
	}
}

func TestPDFMissingRootedPageTreeFallsBackToRealPageDictionaries(t *testing.T) {
	for _, catalog := range []string{"", "<< /Type /Catalog >>", "<< /Type /Catalog /Pages 99 0 R >>"} {
		body := newSyntheticPDF().indirectObject(20, catalog).
			indirectObject(8, "<< /Nested << /Type /Page /Contents 9 0 R >> >>").
			page(1, "/Contents 2 0 R").
			rawStream(2, "", "BT (Real scanned page) Tj ET").
			rawStream(9, "", "BT (Nested page payload) Tj ET").
			trailer("<< /Root 20 0 R >>").finished()
		document, readable := ReadPDF(body)
		if !readable || document.Text != "Real scanned page" {
			t.Fatalf("catalog %q fallback = %t %q", catalog, readable, document.Text)
		}
	}
}

func TestPDFInheritedResourceDepthLimitDoesNotDependOnCacheOrder(t *testing.T) {
	pdf := newSyntheticPDF()
	for reference := 1; reference <= pdfMaxStructureDepth+1; reference++ {
		entries := fmt.Sprintf("/Parent %d 0 R", reference+1)
		if reference == pdfMaxStructureDepth+1 {
			entries = "/Resources << /Font << /Face 100 0 R >> >>"
		}
		pdf.indirectObject(reference, "<< /Type /Pages "+entries+" >>")
	}
	for _, order := range [][]string{{"1 0", "2 0", "3 0"}, {"3 0", "2 0", "1 0"}} {
		lookup := newPDFObjectLookup(pdf.finished())
		for _, reference := range order {
			resources := pdfPageResourceDictionary(lookup.value(reference), lookup)
			want := reference != "1 0"
			if (resources != nil) != want {
				t.Fatalf(
					"order %v reference %s has resources %t, want %t",
					order,
					reference,
					resources != nil,
					want,
				)
			}
		}
	}
}

func TestPDFRootedPageTreeDepthLimitKeepsReachableShallowPages(t *testing.T) {
	pdf := newSyntheticPDF().indirectObject(20, "<< /Type /Catalog /Pages 100 0 R >>").
		page(1, "/Contents 2 0 R").rawStream(2, "", "BT (Shallow page words) Tj ET").
		page(3, "/Contents 4 0 R").rawStream(4, "", "BT (Excessively deep page words) Tj ET")
	for depth := range pdfMaxStructureDepth + 1 {
		kids := fmt.Sprintf("[%d 0 R]", 101+depth)
		if depth == 0 {
			kids = "[1 0 R 101 0 R]"
		}
		if depth == pdfMaxStructureDepth {
			kids = "[3 0 R]"
		}
		pdf.indirectObject(100+depth, "<< /Type /Pages /Kids "+kids+" >>")
	}
	body := pdf.trailer("<< /Root 20 0 R >>").finished()
	document, readable := ReadPDF(body)
	if !readable || document.Text != "Shallow page words" {
		t.Fatalf("bounded page tree = %t %q", readable, document.Text)
	}
}

func TestPDFPageAndFormStreamsCarryDistinctResourceScopes(t *testing.T) {
	body := newSyntheticPDF().page(1, "/Contents [2 0 R 3 0 R] /Resources 10 0 R").
		page(4, "/Contents 2 0 R /Resources 11 0 R").
		page(5, "/Contents 2 0 R /Resources 10 0 R").
		rawStream(2, "", "first").rawStream(3, "", "second").
		indirectObject(10, "<< /Font << /First 20 0 R >> /XObject << /Form 8 0 R >> >>").
		indirectObject(11, "<< /Font << /Second 21 0 R >> /XObject << /Form 8 0 R >> >>").
		rawStream(8, "/Subtype /Form", "form").finished()
	streams := pdfPageDescriptionStreams(body)
	if len(streams) != 6 {
		t.Fatalf("scoped streams = %d, want 6", len(streams))
	}
	if streams[0].scope != streams[1].scope || streams[0].scope == streams[2].scope ||
		streams[0].scope == streams[3].scope || streams[4].scope == streams[5].scope {
		t.Fatalf("page/form scope identities = %+v", streams)
	}
	for index, name := range []string{"First", "First", "Second", "First", "First", "Second"} {
		if !strings.Contains(string(streams[index].resources), "/"+name) {
			t.Fatalf("stream %d resources = %q, want %s", index, streams[index].resources, name)
		}
	}
}
