package crawlurls

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type recordingURLDirectory struct {
	rows  map[yagomodel.Hash]yagomodel.URIMetadataRow
	calls [][]yagomodel.Hash
	errs  map[int]error
}

func (r *recordingURLDirectory) RowsByHash(
	_ context.Context,
	hashes []yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	r.calls = append(r.calls, slices.Clone(hashes))
	if err := r.errs[len(r.calls)]; err != nil {
		return nil, err
	}

	rows := make([]yagomodel.URIMetadataRow, 0, len(hashes))
	for _, hash := range hashes {
		if row, ok := r.rows[hash]; ok {
			rows = append(rows, row)
		}
	}

	return rows, nil
}

type recordingRemoteCrawlURLs struct {
	items   []RemoteCrawlURL
	err     error
	peer    yagomodel.Hash
	limit   int
	timeout time.Duration
	called  bool
}

func (r *recordingRemoteCrawlURLs) URLsForRemoteCrawl(
	_ context.Context,
	peer yagomodel.Hash,
	limit int,
	timeout time.Duration,
) ([]RemoteCrawlURL, error) {
	r.called = true
	r.peer = peer
	r.limit = limit
	r.timeout = timeout

	return r.items, r.err
}

func TestDisabledRemoteCrawlURLsReturnsNoWork(t *testing.T) {
	items, err := DisabledRemoteCrawlURLs{}.URLsForRemoteCrawl(
		t.Context(),
		hashA,
		remoteDefaultCount,
		remoteDefaultTime*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %v, want none", items)
	}
}

func TestRemoteCrawlReturnsEmptySuccessByDefault(t *testing.T) {
	endpoint := newEndpoint(localIdentity(), &recordingURLDirectory{}, nil)
	endpoint.now = fixedNow

	resp, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Iam:         hashA.String(),
		Call:        yagoproto.CrawlURLCallRemoteCrawl,
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != crawlURLContentType {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}
	if !strings.Contains(resp.Body, "<response>ok</response>") {
		t.Fatalf("Body = %q", resp.Body)
	}
	if strings.Contains(resp.Body, "<item>") {
		t.Fatalf("Body has remote item: %q", resp.Body)
	}
}

func TestRemoteCrawlPassesClampedLimitsAndRendersItems(t *testing.T) {
	remote := &recordingRemoteCrawlURLs{
		items: []RemoteCrawlURL{{
			Link:        "https://example.com/?a=1&b=2",
			Referrer:    "https://ref.example/",
			Description: "Remote URL",
			PublishedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			GUID:        hashA,
		}},
	}
	endpoint := newEndpoint(localIdentity(), &recordingURLDirectory{}, remote)
	endpoint.now = fixedNow

	resp, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Iam:         hashA.String(),
		Call:        yagoproto.CrawlURLCallRemoteCrawl,
		Count:       yagomodel.Some(200),
		Time:        yagomodel.Some(500),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !remote.called || remote.peer != hashA || remote.limit != remoteMaxCount ||
		remote.timeout != time.Second {
		t.Fatalf(
			"remote call = %v %q %d %v",
			remote.called,
			remote.peer,
			remote.limit,
			remote.timeout,
		)
	}
	for _, want := range []string{
		"<link>https://example.com/?a=1&amp;b=2</link>",
		"<referrer>https://ref.example/</referrer>",
		"<description>Remote URL</description>",
		"<pubDate>20260102030405</pubDate>",
		"<guid isPermaLink=\"false\">AAAAAAAAAAAA</guid>",
	} {
		if !strings.Contains(resp.Body, want) {
			t.Fatalf("Body missing %q: %q", want, resp.Body)
		}
	}
}

func TestRemoteCrawlLeavesUnknownPublicationDateEmpty(t *testing.T) {
	items := remoteCrawlItems([]RemoteCrawlURL{{Link: "https://example.com/"}})
	if len(items) != 1 || items[0].PubDate != "" {
		t.Fatalf("remote crawl items = %+v", items)
	}
}

func TestRemoteCrawlSurfacesRemoteFailure(t *testing.T) {
	want := errors.New("frontier unavailable")
	endpoint := newEndpoint(
		localIdentity(),
		&recordingURLDirectory{},
		&recordingRemoteCrawlURLs{err: want},
	)
	endpoint.now = fixedNow

	_, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Iam:         hashA.String(),
		Call:        yagoproto.CrawlURLCallRemoteCrawl,
	})

	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestRemoteCrawlRendersPolicyRejectionWithoutInternalFailure(t *testing.T) {
	endpoint := newEndpoint(
		localIdentity(),
		&recordingURLDirectory{},
		&recordingRemoteCrawlURLs{err: ErrRemoteCrawlRejected},
	)
	endpoint.now = fixedNow
	response, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Iam:         hashA.String(),
		Call:        yagoproto.CrawlURLCallRemoteCrawl,
	})
	if err != nil || !strings.Contains(
		response.Body,
		"<response>"+yagoproto.CrawlURLResponseRejected+"</response>",
	) {
		t.Fatalf("policy rejection = %q, %v", response.Body, err)
	}
}

func TestCrawlURLRejectsForeignNetworkAndUnknownCall(t *testing.T) {
	endpoint := newEndpoint(
		localIdentity(),
		&recordingURLDirectory{},
		&recordingRemoteCrawlURLs{},
	)
	endpoint.now = fixedNow

	for _, req := range []yagoproto.CrawlURLRequest{
		{NetworkName: "other", Call: yagoproto.CrawlURLCallRemoteCrawl},
		{NetworkName: "freeworld", Call: "unknown"},
		{NetworkName: "freeworld", Call: yagoproto.CrawlURLCallRemoteCrawl, Iam: "invalid"},
	} {
		resp, err := endpoint.Serve(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(
			resp.Body,
			"<response>"+yagoproto.CrawlURLResponseRejected+"</response>",
		) {
			t.Fatalf("Body = %q", resp.Body)
		}
	}
}

func TestURLHashListReturnsLocalMetadataAndResolvedReferrer(t *testing.T) {
	dir := &recordingURLDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
		hashA: metadataRow(metadataRowFixture{
			hash:     hashA,
			link:     "https://example.com/a",
			title:    "Title A",
			author:   "Author A",
			modified: "20260102",
			referrer: hashB,
		}),
		hashB: metadataRow(metadataRowFixture{
			hash:     hashB,
			link:     "https://example.com/ref",
			title:    "Referrer",
			modified: "20260102030405",
		}),
		hashC: metadataRow(metadataRowFixture{
			hash:     hashC,
			link:     "https://example.com/c",
			title:    "Title C",
			modified: "20260102030405",
		}),
	}}
	endpoint := newEndpoint(localIdentity(), dir, nil)
	endpoint.now = fixedNow

	resp, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Call:        yagoproto.CrawlURLCallURLHashList,
		Hashes:      hashA.String() + "ZZZZZZZZZZZZ" + hashC.String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(dir.calls) != 2 {
		t.Fatalf("directory calls = %d, want 2", len(dir.calls))
	}
	if !slices.Equal(dir.calls[0], []yagomodel.Hash{hashA, "ZZZZZZZZZZZZ", hashC}) {
		t.Fatalf("first call = %v", dir.calls[0])
	}
	if !slices.Equal(dir.calls[1], []yagomodel.Hash{hashB}) {
		t.Fatalf("second call = %v", dir.calls[1])
	}
	for _, want := range []string{
		"<response>ok</response>",
		"<title>Title A</title>",
		"<link>https://example.com/a</link>",
		"<referrer>https://example.com/ref</referrer>",
		"<description>Title A</description>",
		"<author>Author A</author>",
		"<pubDate>20260102000000</pubDate>",
		"<guid isPermaLink=\"false\">AAAAAAAAAAAA</guid>",
		"<title>Title C</title>",
		"<pubDate>20260102030405</pubDate>",
	} {
		if !strings.Contains(resp.Body, want) {
			t.Fatalf("Body missing %q: %q", want, resp.Body)
		}
	}
}

func TestURLHashListRejectsOversizedInputBeforeDirectoryLookup(t *testing.T) {
	directory := &recordingURLDirectory{}
	endpoint := newEndpoint(localIdentity(), directory, nil)
	response, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Call:        yagoproto.CrawlURLCallURLHashList,
		Hashes: strings.Repeat(
			hashA.String(),
			yagoproto.MaximumCrawlURLHashes+1,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(directory.calls) != 0 {
		t.Fatalf("directory calls = %d, want none", len(directory.calls))
	}
	if !strings.Contains(response.Body, yagoproto.CrawlURLResponseRejected) {
		t.Fatalf("body = %q, want rejected response", response.Body)
	}
}

func TestURLHashListReturnsOKWhenNoHashesAreRequested(t *testing.T) {
	dir := &recordingURLDirectory{}
	endpoint := newEndpoint(localIdentity(), dir, nil)
	endpoint.now = fixedNow

	resp, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Call:        yagoproto.CrawlURLCallURLHashList,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(dir.calls) != 1 || len(dir.calls[0]) != 0 {
		t.Fatalf("directory calls = %v", dir.calls)
	}
	if !strings.Contains(resp.Body, "<response>ok</response>") {
		t.Fatalf("Body = %q", resp.Body)
	}
}

func TestURLHashListRejectsBadHashListLength(t *testing.T) {
	dir := &recordingURLDirectory{}
	endpoint := newEndpoint(localIdentity(), dir, nil)
	endpoint.now = fixedNow

	resp, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Call:        yagoproto.CrawlURLCallURLHashList,
		Hashes:      "short",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(dir.calls) != 0 {
		t.Fatalf("directory was called: %v", dir.calls)
	}
	if !strings.Contains(resp.Body, "<response>"+yagoproto.CrawlURLResponseRejected+"</response>") {
		t.Fatalf("Body = %q", resp.Body)
	}
}

func TestURLHashListSurfacesDirectoryFailure(t *testing.T) {
	want := errors.New("vault failed")
	endpoint := newEndpoint(
		localIdentity(),
		&recordingURLDirectory{errs: map[int]error{1: want}},
		nil,
	)
	endpoint.now = fixedNow

	_, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Call:        yagoproto.CrawlURLCallURLHashList,
		Hashes:      hashA.String(),
	})

	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestURLHashListSurfacesReferrerDirectoryFailure(t *testing.T) {
	want := errors.New("referrer vault failed")
	endpoint := newEndpoint(
		localIdentity(),
		&recordingURLDirectory{
			rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				hashA: metadataRow(metadataRowFixture{
					hash:     hashA,
					link:     "https://example.com/a",
					title:    "Title A",
					modified: "20260102",
					referrer: hashB,
				}),
			},
			errs: map[int]error{2: want},
		},
		nil,
	)
	endpoint.now = fixedNow

	_, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
		NetworkName: "freeworld",
		Call:        yagoproto.CrawlURLCallURLHashList,
		Hashes:      hashA.String(),
	})

	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestURLHashListSurfacesBadReferrerRows(t *testing.T) {
	t.Run("missing hash", func(t *testing.T) {
		endpoint := newEndpoint(
			localIdentity(),
			&recordingURLDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				hashA: metadataRow(metadataRowFixture{
					hash:     hashA,
					link:     "https://example.com/a",
					title:    "Title A",
					modified: "20260102",
					referrer: hashB,
				}),
				hashB: {Properties: map[string]string{
					yagomodel.URLMetaURL: yagomodel.EncodeBase64WireForm("https://example.com/ref"),
				}},
			}},
			nil,
		)
		endpoint.now = fixedNow

		_, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
			NetworkName: "freeworld",
			Call:        yagoproto.CrawlURLCallURLHashList,
			Hashes:      hashA.String(),
		})
		if err == nil {
			t.Fatal("expected bad referrer hash error")
		}
	})

	t.Run("bad url", func(t *testing.T) {
		endpoint := newEndpoint(
			localIdentity(),
			&recordingURLDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				hashA: metadataRow(metadataRowFixture{
					hash:     hashA,
					link:     "https://example.com/a",
					title:    "Title A",
					modified: "20260102",
					referrer: hashB,
				}),
				hashB: {Properties: map[string]string{
					yagomodel.URLMetaHash: hashB.String(),
					yagomodel.URLMetaURL:  "z|@@@",
				}},
			}},
			nil,
		)
		endpoint.now = fixedNow

		_, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
			NetworkName: "freeworld",
			Call:        yagoproto.CrawlURLCallURLHashList,
			Hashes:      hashA.String(),
		})
		if err == nil {
			t.Fatal("expected bad referrer url error")
		}
	})
}

func TestURLHashListSurfacesBadMetadataRows(t *testing.T) {
	t.Run("bad wire field", func(t *testing.T) {
		endpoint := newEndpoint(
			localIdentity(),
			&recordingURLDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				hashA: {Properties: map[string]string{
					yagomodel.URLMetaHash:           hashA.String(),
					yagomodel.URLMetaColDescription: "z|@@@",
				}},
			}},
			nil,
		)
		endpoint.now = fixedNow

		_, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
			NetworkName: "freeworld",
			Call:        yagoproto.CrawlURLCallURLHashList,
			Hashes:      hashA.String(),
		})
		if err == nil {
			t.Fatal("expected bad metadata wire error")
		}
	})

	t.Run("missing hash", func(t *testing.T) {
		endpoint := newEndpoint(
			localIdentity(),
			&recordingURLDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				hashA: {Properties: map[string]string{
					yagomodel.URLMetaURL: yagomodel.EncodeBase64WireForm(
						"https://example.com/a",
					),
					yagomodel.URLMetaColDescription: yagomodel.EncodeBase64WireForm("Title A"),
				}},
			}},
			nil,
		)
		endpoint.now = fixedNow

		_, err := endpoint.Serve(t.Context(), yagoproto.CrawlURLRequest{
			NetworkName: "freeworld",
			Call:        yagoproto.CrawlURLCallURLHashList,
			Hashes:      hashA.String(),
		})
		if err == nil {
			t.Fatal("expected missing hash error")
		}
	})
}

func TestRemoteLimitDefaultsAndBounds(t *testing.T) {
	if got := remoteURLCount(yagomodel.None[int]()); got != remoteDefaultCount {
		t.Fatalf("default count = %d", got)
	}
	if got := remoteURLCount(yagomodel.Some(7)); got != 7 {
		t.Fatalf("count = %d, want 7", got)
	}
	if got := remoteURLCount(yagomodel.Some(-1)); got != 0 {
		t.Fatalf("negative count = %d", got)
	}
	if got := remoteURLTimeout(yagomodel.None[int]()); got != 10*time.Second {
		t.Fatalf("default timeout = %v", got)
	}
	if got := remoteURLTimeout(yagomodel.Some(7000)); got != 7*time.Second {
		t.Fatalf("timeout = %v, want 7s", got)
	}
	if got := remoteURLTimeout(yagomodel.Some(30000)); got != 20*time.Second {
		t.Fatalf("max timeout = %v", got)
	}
}

func TestEncodeCrawlURLFeedEscapesVersionAttribute(t *testing.T) {
	body := encodeCrawlURLFeed(crawlURLFeed{
		Version:  `1"&<>`,
		Iam:      hashA.String(),
		Response: yagoproto.CrawlURLResponseRejected,
	})

	if !strings.Contains(body, `<yacy version="1&quot;&amp;&lt;&gt;">`) {
		t.Fatalf("Body = %q", body)
	}
}

func TestMountServesCrawlURLRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	dir := &recordingURLDirectory{}
	Mount(router, localIdentity(), dir, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathCrawlURLs+"?"+yagoproto.CrawlURLRequest{
			NetworkName: "freeworld",
			Call:        yagoproto.CrawlURLCallURLHashList,
		}.Form().Encode(),
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if len(dir.calls) != 1 {
		t.Fatalf("directory calls = %d, want 1", len(dir.calls))
	}
}

func localIdentity() nodeidentity.Identity {
	now := fixedNow()

	return nodeidentity.Identity{
		Hash:        hashA,
		NetworkName: "freeworld",
		Version:     "1.940",
		Start:       now.Add(-42 * time.Minute),
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

type metadataRowFixture struct {
	hash     yagomodel.Hash
	link     string
	title    string
	author   string
	modified string
	referrer yagomodel.Hash
}

func metadataRow(fixture metadataRowFixture) yagomodel.URIMetadataRow {
	properties := map[string]string{
		yagomodel.URLMetaHash:           fixture.hash.String(),
		yagomodel.URLMetaURL:            yagomodel.EncodeBase64WireForm(fixture.link),
		yagomodel.URLMetaColDescription: yagomodel.EncodeBase64WireForm(fixture.title),
		yagomodel.ColModDate:            fixture.modified,
	}
	if fixture.author != "" {
		properties[yagomodel.URLMetaAuthor] = yagomodel.EncodeBase64WireForm(fixture.author)
	}
	if fixture.referrer != "" {
		properties[yagomodel.URLMetaReferrer] = fixture.referrer.String()
	}

	return yagomodel.URIMetadataRow{Properties: properties}
}

const (
	hashA yagomodel.Hash = "AAAAAAAAAAAA"
	hashB yagomodel.Hash = "BBBBBBBBBBBB"
	hashC yagomodel.Hash = "CCCCCCCCCCCC"
)
