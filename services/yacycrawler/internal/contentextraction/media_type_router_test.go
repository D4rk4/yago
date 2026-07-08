package contentextraction_test

import (
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/contentextraction"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

type fakeExtractor struct {
	contents []crawlcapability.ExtractedContent
	err      error
	gotURL   string
}

func (f *fakeExtractor) Extract(
	pageURL, _ string,
	_ []byte,
) ([]crawlcapability.ExtractedContent, error) {
	f.gotURL = pageURL
	return f.contents, f.err
}

type fakeContainer struct {
	members []crawlcapability.ArchiveMember
	err     error
}

func (f *fakeContainer) Expand(_, _ string, _ []byte) ([]crawlcapability.ArchiveMember, error) {
	return f.members, f.err
}

func TestExtractDispatchesToRegisteredExtractor(t *testing.T) {
	extractor := &fakeExtractor{contents: []crawlcapability.ExtractedContent{{Title: "page"}}}
	router := contentextraction.New(4, 16)
	router.RegisterExtractor("text/html", extractor)

	documents, err := router.Extract("http://host/p", "text/html; charset=utf-8", []byte("x"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(documents) != 1 || documents[0].Title != "page" {
		t.Fatalf("unexpected documents: %+v", documents)
	}
	if extractor.gotURL != "http://host/p" {
		t.Fatalf("extractor got url %q", extractor.gotURL)
	}
}

func TestExtractUnsupportedMediaType(t *testing.T) {
	router := contentextraction.New(4, 16)
	router.RegisterExtractor("text/html", &fakeExtractor{})

	_, err := router.Extract("http://host/f", "application/pdf", []byte("x"))
	if !errors.Is(err, crawlcapability.ErrUnsupportedMediaType) {
		t.Fatalf("want ErrUnsupportedMediaType, got %v", err)
	}
}

func TestExtractExpandsContainerAndStampsMemberURL(t *testing.T) {
	html := &fakeExtractor{contents: []crawlcapability.ExtractedContent{{Title: "member"}}}
	container := &fakeContainer{members: []crawlcapability.ArchiveMember{
		{URL: "http://host/a.zip!/one.html", ContentType: "text/html", Body: []byte("1")},
		{URL: "http://host/a.zip!/skip.bin", ContentType: "application/octet-stream"},
	}}
	router := contentextraction.New(4, 16)
	router.RegisterExtractor("text/html", html)
	router.RegisterContainer("application/zip", container)

	documents, err := router.Extract("http://host/a.zip", "application/zip", []byte("x"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("want 1 document (unsupported member skipped), got %d", len(documents))
	}
	if documents[0].URL != "http://host/a.zip!/one.html" {
		t.Fatalf("member url not stamped: %q", documents[0].URL)
	}
}

func TestExtractNestedContainerExpands(t *testing.T) {
	html := &fakeExtractor{contents: []crawlcapability.ExtractedContent{{Title: "deep"}}}
	tar := &fakeContainer{members: []crawlcapability.ArchiveMember{
		{URL: "u!/inner.tar!/p.html", ContentType: "text/html"},
	}}
	zip := &fakeContainer{members: []crawlcapability.ArchiveMember{
		{URL: "u!/inner.tar", ContentType: "application/x-tar"},
	}}
	router := contentextraction.New(4, 16)
	router.RegisterExtractor("text/html", html)
	router.RegisterContainer("application/zip", zip)
	router.RegisterContainer("application/x-tar", tar)

	documents, err := router.Extract("u", "application/zip", []byte("x"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(documents) != 1 || documents[0].Title != "deep" {
		t.Fatalf("nested expansion unexpected: %+v", documents)
	}
}

func TestExtractNestingDepthOverflow(t *testing.T) {
	selfContainer := &fakeContainer{members: []crawlcapability.ArchiveMember{
		{URL: "u!/again.zip", ContentType: "application/zip"},
	}}
	router := contentextraction.New(2, 16)
	router.RegisterContainer("application/zip", selfContainer)

	_, err := router.Extract("u", "application/zip", []byte("x"))
	if !errors.Is(err, crawlcapability.ErrContainerOverflow) {
		t.Fatalf("want ErrContainerOverflow, got %v", err)
	}
}

func TestExtractDocumentsPerContainerOverflow(t *testing.T) {
	html := &fakeExtractor{contents: []crawlcapability.ExtractedContent{{Title: "m"}}}
	container := &fakeContainer{members: []crawlcapability.ArchiveMember{
		{URL: "u!/1.html", ContentType: "text/html"},
		{URL: "u!/2.html", ContentType: "text/html"},
		{URL: "u!/3.html", ContentType: "text/html"},
	}}
	router := contentextraction.New(4, 2)
	router.RegisterExtractor("text/html", html)
	router.RegisterContainer("application/zip", container)

	_, err := router.Extract("u", "application/zip", []byte("x"))
	if !errors.Is(err, crawlcapability.ErrContainerOverflow) {
		t.Fatalf("want ErrContainerOverflow, got %v", err)
	}
}

func TestExtractContainerExpandError(t *testing.T) {
	container := &fakeContainer{err: errors.New("corrupt")}
	router := contentextraction.New(4, 16)
	router.RegisterContainer("application/zip", container)

	_, err := router.Extract("u", "application/zip", []byte("x"))
	if err == nil {
		t.Fatal("want error from expand")
	}
}

func TestRegisteredMediaTypes(t *testing.T) {
	router := contentextraction.New(4, 16)
	router.RegisterExtractor("text/html", &fakeExtractor{})
	router.RegisterContainer("application/zip", &fakeContainer{})
	if router.RegisteredMediaTypes() != 2 {
		t.Fatalf("want 2, got %d", router.RegisteredMediaTypes())
	}
}
