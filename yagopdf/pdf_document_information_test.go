package yagopdf

import "testing"

func TestReadPDFTitleComesFromDocumentInformationNotOutline(t *testing.T) {
	content := `BT /F1 12 Tf 72 700 Td (Synthetic installation steps) Tj ET`
	body := append(
		pdfWithText(t, "Outline entry", content),
		"\n9 0 obj\n<< /Title (Synthetic Guide) >>\nendobj\n"+
			"trailer\n<< /Size 10 /Info 9 0 R >>\n%%EOF"...,
	)
	document, parsed := ReadPDF(body)
	if !parsed || document.Title != "Synthetic Guide" {
		t.Fatalf("pdf read = %v %+v", parsed, document)
	}
}

func TestPDFInfoTitlePrefersTrailerDocumentInformation(t *testing.T) {
	const outline = "%PDF-1.7\n1 0 obj\n<< /Title (Contents) /Parent 2 0 R >>\nendobj\n"
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "trailer information after outline",
			body: outline + "9 0 obj\n<< /Producer (Synthetic) /Title (Synthetic Guide) >>\nendobj\n" +
				"trailer\n<< /Size 10 /Info 9 0 R >>\n",
			want: "Synthetic Guide",
		},
		{
			name: "cross-reference stream information",
			body: outline + "9 0 obj\n<< /Title <FEFF00470075006900640065> >>\nendobj\n" +
				"10 0 obj\n<< /Type /XRef /Info 9 0 R /Length 0 >>\nstream\n\nendstream\nendobj\n",
			want: "Guide",
		},
		{
			name: "newest trailer wins",
			body: outline + "8 0 obj\n<< /Title (Old) >>\nendobj\n9 0 obj\n<< /Title (New) >>\nendobj\n" +
				"trailer\n<< /Info 8 0 R >>\ntrailer\n<< /Info 9 0 R /InfoX (ignored) >>\n",
			want: "New",
		},
		{
			name: "information without title refuses outline",
			body: outline + "9 0 obj\n<< /Producer (Synthetic) >>\nendobj\n" +
				"trailer\n<< /Info 9 0 R >>\n",
			want: "",
		},
		{
			name: "missing information object refuses outline",
			body: outline + "trailer\n<< /Info 9 0 R >>\n",
			want: "",
		},
		{
			name: "no information reference falls back to first title",
			body: outline + "trailer\n<< /Size 2 >>\n",
			want: "Contents",
		},
		{
			name: "information without reference falls back to first title",
			body: outline + "trailer\n<< /Info (inline) >>\n",
			want: "Contents",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pdfInfoTitle([]byte(test.body)); got != test.want {
				t.Fatalf("title = %q, want %q", got, test.want)
			}
		})
	}
}
