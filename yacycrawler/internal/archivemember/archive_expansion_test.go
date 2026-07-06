package archivemember_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/archivemember"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func tarBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	for name, content := range entries {
		if err := writer.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buf.Bytes()
}

func TestExpandZipMembers(t *testing.T) {
	body := zipBytes(t, map[string]string{"page.html": "<html>hi</html>"})
	members, err := archivemember.New(16, 1<<20).
		Expand("http://host/a.zip", "application/zip", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("want 1 member, got %d", len(members))
	}
	if members[0].URL != "http://host/a.zip!/page.html" {
		t.Fatalf("member URL: %q", members[0].URL)
	}
	if members[0].ContentType != "text/html; charset=utf-8" {
		t.Fatalf("member content type: %q", members[0].ContentType)
	}
}

func TestExpandTarMembers(t *testing.T) {
	body := tarBytes(t, map[string]string{"doc.html": "<html>hi</html>"})
	members, err := archivemember.New(16, 1<<20).
		Expand("http://host/a.tar", "application/x-tar", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 1 || members[0].URL != "http://host/a.tar!/doc.html" {
		t.Fatalf("unexpected members: %+v", members)
	}
}

func TestExpandGzipTar(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(tarBytes(t, map[string]string{"a.html": "x"})); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	members, err := archivemember.New(16, 1<<20).
		Expand("http://host/a.tar.gz", "application/gzip", buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 1 || members[0].URL != "http://host/a.tar.gz!/a.tar" {
		t.Fatalf("unexpected members: %+v", members)
	}
	if members[0].ContentType != "application/x-tar" {
		t.Fatalf("unexpected content type: %q", members[0].ContentType)
	}
}

func TestExpandGzipSingleFile(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte("<html>hi</html>")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	members, err := archivemember.New(16, 1<<20).
		Expand("http://host/page.html.gz", "application/gzip", buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 1 || members[0].URL != "http://host/page.html.gz!/page.html" {
		t.Fatalf("unexpected members: %+v", members)
	}
	if members[0].ContentType != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", members[0].ContentType)
	}
	if string(members[0].Body) != "<html>hi</html>" {
		t.Fatalf("unexpected body: %q", members[0].Body)
	}
}

func TestExpandSkipsUnknownExtensionAndDirectories(t *testing.T) {
	body := zipBytes(t, map[string]string{
		"page.html":     "x",
		"data.unknownx": "y",
	})
	members, err := archivemember.New(16, 1<<20).
		Expand("u", "application/zip", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("want only the html member, got %d", len(members))
	}
}

func TestExpandSkipsOversizedMember(t *testing.T) {
	body := zipBytes(t, map[string]string{"big.html": "0123456789"})
	members, err := archivemember.New(16, 4).Expand("u", "application/zip", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("want oversized member skipped, got %d", len(members))
	}
}

func TestExpandTooManyMembersOverflows(t *testing.T) {
	body := zipBytes(t, map[string]string{
		"a.html": "1", "b.html": "2", "c.html": "3",
	})
	_, err := archivemember.New(2, 1<<20).Expand("u", "application/zip", body)
	if !errors.Is(err, crawlcapability.ErrContainerOverflow) {
		t.Fatalf("want ErrContainerOverflow, got %v", err)
	}
}

func TestExpandUnsupportedMediaType(t *testing.T) {
	_, err := archivemember.New(16, 1<<20).Expand("u", "application/pdf", []byte("x"))
	if !errors.Is(err, crawlcapability.ErrUnsupportedMediaType) {
		t.Fatalf("want ErrUnsupportedMediaType, got %v", err)
	}
}

func TestExpandCorruptZip(t *testing.T) {
	_, err := archivemember.New(16, 1<<20).Expand("u", "application/zip", []byte("not a zip"))
	if err == nil {
		t.Fatal("want error for corrupt zip")
	}
}

func TestMediaTypesDeclared(t *testing.T) {
	got := archivemember.New(1, 1).MediaTypes()
	if len(got) != 3 {
		t.Fatalf("want 3 media types, got %v", got)
	}
}
