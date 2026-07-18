package formatparse

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type urlFetchAdmissionCase struct {
	name    string
	url     string
	formats yagocrawlcontract.FormatToggles
	want    bool
}

func TestURLFetchAllowedUsesRegisteredFormats(t *testing.T) {
	enabled := yagocrawlcontract.DefaultFormatToggles()
	enabled.Archives = true
	disabledArchive := enabled
	disabledArchive.Archives = false
	disabledOffice := enabled
	disabledOffice.Office = false

	assertURLFetchAdmissions(t, []urlFetchAdmissionCase{
		{
			name:    "enabled archive",
			url:     "https://example.com/release.zip",
			formats: enabled,
			want:    true,
		},
		{
			name:    "disabled archive",
			url:     "https://example.com/release.zip",
			formats: disabledArchive,
		},
		{
			name:    "uppercase archive",
			url:     "https://example.com/release.ZIP?signature=1",
			formats: disabledArchive,
		},
		{
			name:    "double archive extension",
			url:     "https://example.com/release.tar.gz",
			formats: disabledArchive,
		},
		{
			name:    "enabled double archive extension",
			url:     "https://example.com/release.tar.gz",
			formats: enabled,
			want:    true,
		},
		{
			name:    "enabled office",
			url:     "https://example.com/guide.docx",
			formats: enabled,
			want:    true,
		},
		{name: "disabled office", url: "https://example.com/guide.docx", formats: disabledOffice},
	})
}

func TestURLFetchAllowedRejectsUnsupportedAndKeepsAmbiguousRoutes(t *testing.T) {
	enabled := yagocrawlcontract.DefaultFormatToggles()
	enabled.Archives = true
	disabledArchive := enabled
	disabledArchive.Archives = false

	assertURLFetchAdmissions(t, []urlFetchAdmissionCase{
		{name: "unsupported appimage", url: "https://example.com/tool.appimage", formats: enabled},
		{name: "unsupported deb", url: "https://example.com/tool.deb", formats: enabled},
		{name: "unsupported dmg", url: "https://example.com/tool.dmg", formats: enabled},
		{name: "unsupported exe", url: "https://example.com/tool.exe", formats: enabled},
		{name: "unsupported iso", url: "https://example.com/disk.iso", formats: enabled},
		{name: "unsupported msi", url: "https://example.com/setup.msi", formats: enabled},
		{name: "unsupported pkg", url: "https://example.com/setup.pkg", formats: enabled},
		{name: "unsupported rpm", url: "https://example.com/tool.rpm", formats: enabled},
		{
			name:    "unsupported compressed tar",
			url:     "https://example.com/release.tar.xz",
			formats: enabled,
		},
		{
			name:    "extensionless download",
			url:     "https://example.com/download?file=release.zip",
			formats: disabledArchive,
			want:    true,
		},
		{
			name:    "unknown extension",
			url:     "https://example.com/content.route",
			formats: disabledArchive,
			want:    true,
		},
		{
			name:    "html suffix",
			url:     "https://example.com/release.pkg.html",
			formats: disabledArchive,
			want:    true,
		},
		{
			name:    "directory after suffix",
			url:     "https://example.com/release.zip/index",
			formats: disabledArchive,
			want:    true,
		},
	})
}

func assertURLFetchAdmissions(t *testing.T, tests []urlFetchAdmissionCase) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := URLFetchAllowed(test.url, test.formats); got != test.want {
				t.Fatalf("URLFetchAllowed(%q) = %t, want %t", test.url, got, test.want)
			}
		})
	}
}

func BenchmarkURLFetchAllowed(b *testing.B) {
	formats := yagocrawlcontract.DefaultFormatToggles()
	for b.Loop() {
		if URLFetchAllowed("https://example.com/releases/archive.tar.gz?signature=1", formats) {
			b.Fatal("disabled archive admitted")
		}
	}
}
