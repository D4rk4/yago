package adminui

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

type adminAssetReadFailureFS struct {
	fs.FS
	err error
}

func (assets adminAssetReadFailureFS) Open(name string) (fs.File, error) {
	if name == "assets/carbon.css" {
		return nil, assets.err
	}
	file, err := assets.FS.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open admin asset %q: %w", name, err)
	}

	return file, nil
}

func TestAdminAssetReferencesChangeWithContent(t *testing.T) {
	t.Parallel()

	first, err := buildAdminAssetReferences(fstest.MapFS{
		"assets/carbon.css": {Data: []byte("first")},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildAdminAssetReferences(fstest.MapFS{
		"assets/carbon.css": {Data: []byte("second")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first["carbon.css"] == second["carbon.css"] {
		t.Fatal("asset reference did not change with content")
	}
	if !strings.HasPrefix(first["carbon.css"], "/admin/assets/carbon.css?v=") ||
		len(
			strings.TrimPrefix(first["carbon.css"], "/admin/assets/carbon.css?v="),
		) != adminAssetRevisionBytes*2 {
		t.Fatalf("asset reference = %q", first["carbon.css"])
	}
}

func TestAdminAssetReferenceFailures(t *testing.T) {
	t.Parallel()

	if _, err := buildAdminAssetReferences(fstest.MapFS{}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing asset root error = %v", err)
	}
	readErr := errors.New("asset read failed")
	broken := adminAssetReadFailureFS{
		FS:  fstest.MapFS{"assets/carbon.css": {Data: []byte("body")}},
		err: readErr,
	}
	if _, err := buildAdminAssetReferences(broken); !errors.Is(err, readErr) {
		t.Fatalf("asset read error = %v", err)
	}

	functions := adminAssetTemplateFunctions()
	asset := functions["asset"].(func(string) (string, error))
	reference, err := asset("carbon.css")
	if err != nil || reference != embeddedAdminAssetCatalog["carbon.css"].reference {
		t.Fatalf("embedded template asset = %q, %v", reference, err)
	}
	if _, err := asset("missing.css"); err == nil {
		t.Fatal("missing template asset succeeded")
	}
}

func TestMustAdminAssetReferencesPanicsOnInvalidFilesystem(t *testing.T) {
	t.Parallel()

	deferred := false
	func() {
		defer func() {
			deferred = recover() != nil
		}()
		mustAdminAssetReferences(fstest.MapFS{})
	}()
	if !deferred {
		t.Fatal("invalid asset filesystem did not panic")
	}
}

func TestVersionedAdminAssetReferenceIsServed(t *testing.T) {
	t.Parallel()

	reference := mustAdminAssetReferences(assetFS)["carbon.css"]
	got := do(t, New(Options{}), reference)
	if got.status != http.StatusOK || !strings.Contains(got.body, "--cds-interactive") {
		t.Fatalf("versioned asset = %d %.80q", got.status, got.body)
	}
	if got.header.Get("Cache-Control") != adminAssetImmutableCacheControl {
		t.Fatalf("asset cache policy = %q", got.header.Get("Cache-Control"))
	}
	if got.header.Get("ETag") == "" || got.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("asset headers = %v", got.header)
	}
}

func TestUnversionedAdminAssetRequiresRevalidation(t *testing.T) {
	t.Parallel()

	console := New(Options{})
	first := do(t, console, adminAssetPath+"carbon.css")
	if first.status != http.StatusOK ||
		first.header.Get("Cache-Control") != adminAssetRevalidationCacheControl ||
		first.header.Get("ETag") == "" {
		t.Fatalf("unversioned asset = %d %v", first.status, first.header)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		adminAssetPath+"carbon.css",
		nil,
	)
	request.Header.Set("If-None-Match", first.header.Get("ETag"))
	recorder := httptest.NewRecorder()
	console.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotModified ||
		recorder.Header().Get("Cache-Control") != adminAssetRevalidationCacheControl {
		t.Fatalf("revalidation = %d %v", recorder.Code, recorder.Header())
	}
}

func TestAdminAssetRejectsNoncurrentQueriesAndPathAliases(t *testing.T) {
	t.Parallel()

	references := mustAdminAssetReferences(assetFS)
	carbonReference := references["carbon.css"]
	carbonQuery := strings.SplitN(carbonReference, "?", 2)[1]
	photonQuery := strings.SplitN(references["photon.css"], "?", 2)[1]
	console := New(Options{})
	tests := []struct {
		name   string
		target string
		mutate func(*http.Request)
	}{
		{name: "wrong revision", target: adminAssetPath + "carbon.css?v=000000000000000000000000"},
		{name: "cross asset revision", target: adminAssetPath + "carbon.css?" + photonQuery},
		{name: "duplicate revision", target: carbonReference + "&" + carbonQuery},
		{name: "extra query", target: carbonReference + "&mode=old"},
		{
			name:   "malformed query",
			target: adminAssetPath + "carbon.css",
			mutate: func(request *http.Request) { request.URL.RawQuery = "v=%zz" },
		},
		{name: "missing asset", target: adminAssetPath + "missing.css"},
		{name: "directory", target: adminAssetPath + "vendor/"},
		{name: "asset root", target: strings.TrimSuffix(adminAssetPath, "/")},
		{name: "dot segment", target: adminAssetPath + "./carbon.css"},
		{name: "parent segment", target: adminAssetPath + "vendor/../carbon.css"},
		{name: "repeated separator", target: "/admin//assets/carbon.css"},
		{name: "cleaned alias", target: "/alias/../admin/assets/carbon.css"},
		{name: "encoded separator", target: "/admin/assets%2Fcarbon.css"},
		{name: "encoded parent", target: adminAssetPath + "%2e%2e/overview"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				test.target,
				nil,
			)
			if test.mutate != nil {
				test.mutate(request)
			}
			recorder := httptest.NewRecorder()
			console.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNotFound ||
				recorder.Header().Get("Cache-Control") != adminAssetRejectedCacheControl ||
				recorder.Header().Get("Location") != "" ||
				recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("rejection = %d %v", recorder.Code, recorder.Header())
			}
		})
	}
}

func TestAdminAssetAliasFencePassesCanonicalAndUnrelatedPaths(t *testing.T) {
	t.Parallel()

	passed := 0
	surface := RejectAdminAssetAliases(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			passed++
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	for _, target := range []string{adminAssetPath + "carbon.css", "/health"} {
		response := httptest.NewRecorder()
		surface.ServeHTTP(
			response,
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil),
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("canonical path %q = %d", target, response.Code)
		}
	}
	if passed != 2 {
		t.Fatalf("passed requests = %d", passed)
	}

	response := httptest.NewRecorder()
	surface.ServeHTTP(response, httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		adminAssetPath+"./carbon.css",
		nil,
	))
	if response.Code != http.StatusNotFound ||
		response.Header().Get("Cache-Control") != adminAssetRejectedCacheControl {
		t.Fatalf("alias = %d %v", response.Code, response.Header())
	}
}

func TestAdminAssetFileErrorsAreNotCached(t *testing.T) {
	t.Parallel()

	reference := mustAdminAssetReferences(assetFS)["carbon.css"]
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, reference, nil)
	request.Header.Set("Range", "bytes=999999999999-")
	recorder := httptest.NewRecorder()
	New(Options{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestedRangeNotSatisfiable ||
		recorder.Header().Get("Cache-Control") != adminAssetRejectedCacheControl {
		t.Fatalf("range error = %d %v", recorder.Code, recorder.Header())
	}
}
