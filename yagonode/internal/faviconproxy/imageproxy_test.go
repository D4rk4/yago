package faviconproxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func imageGet(t *testing.T, proxy *ImageProxy, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET "+ImagePath, proxy)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

	return rec
}

func TestImageProxyServesAndCachesFetchedImage(t *testing.T) {
	icon := pngBytes(t)
	proxy, hits := originAndImageProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(icon) // nosemgrep
	})

	rec := imageGet(t, proxy, ImageURLFor("https://example.org/pic.png"))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), icon) {
		t.Fatalf("status=%d, want the fetched image", rec.Code)
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Fatal("cache header missing")
	}
	if rec := imageGet(
		t,
		proxy,
		ImageURLFor("https://example.org/pic.png"),
	); !bytes.Equal(
		rec.Body.Bytes(),
		icon,
	) {
		t.Fatal("cached response differs")
	}
	if hits.Load() != 1 {
		t.Fatalf("origin fetched %d times, want 1", hits.Load())
	}
}

func TestImageProxyPlaceholderAndValidation(t *testing.T) {
	proxy, _ := originAndImageProxy(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	rec := imageGet(t, proxy, ImageURLFor("https://example.org/missing.png"))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), proxy.placeholder) {
		t.Fatal("missing image must serve the placeholder")
	}

	svg, _ := originAndImageProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(
			[]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`),
		) // nosemgrep
	})
	if rec := imageGet(
		t,
		svg,
		ImageURLFor("https://example.org/x.svg"),
	); !bytes.Equal(
		rec.Body.Bytes(),
		svg.placeholder,
	) {
		t.Fatal("svg must be rejected to the placeholder")
	}

	oversize, _ := originAndImageProxy(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(append(pngBytes(t), make([]byte, maxImageBytes)...)) // nosemgrep
	})
	if rec := imageGet(
		t,
		oversize,
		ImageURLFor("https://example.org/big.png"),
	); !bytes.Equal(
		rec.Body.Bytes(),
		oversize.placeholder,
	) {
		t.Fatal("oversize image must serve the placeholder")
	}

	bare := NewImageProxy(&http.Client{})
	for _, target := range []string{
		ImagePath,
		ImagePath + "?u=ftp%3A%2F%2Fa.example%2Fx",
		ImagePath + "?u=%2Frelative",
		ImagePath + "?u=https%3A%2F%2Fuser%40host%2Fx",
		ImagePath + "?u=%25zz",
	} {
		if rec := imageGet(t, bare, target); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}

func TestImageProxyTransportErrorAndBusySlots(t *testing.T) {
	proxy := NewImageProxy(&http.Client{Transport: failingTransport{}})
	if rec := imageGet(
		t,
		proxy,
		ImageURLFor("https://example.org/x.png"),
	); !bytes.Equal(
		rec.Body.Bytes(),
		proxy.placeholder,
	) {
		t.Fatal("transport failure must serve the placeholder")
	}

	busy := NewImageProxy(&http.Client{})
	for i := 0; i < imageFetchSlotCount; i++ {
		busy.slots <- struct{}{}
	}
	if rec := imageGet(
		t,
		busy,
		ImageURLFor("https://example.org/x.png"),
	); !bytes.Equal(
		rec.Body.Bytes(),
		busy.placeholder,
	) {
		t.Fatal("busy slots must serve the placeholder")
	}
}

func TestMountImagesSkipsNilClient(t *testing.T) {
	mux := http.NewServeMux()
	MountImages(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, ImageURLFor("https://x.example/p.png"), nil,
	))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil client route status = %d, want 404", rec.Code)
	}

	mounted := http.NewServeMux()
	MountImages(mounted, &http.Client{})
	rec = httptest.NewRecorder()
	mounted.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, ImagePath, nil,
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mounted route without u = %d, want 400", rec.Code)
	}
}

// originAndImageProxy mirrors originAndProxy for the page-image proxy.
func originAndImageProxy(t *testing.T, handler http.HandlerFunc) (*ImageProxy, *atomic.Int64) {
	t.Helper()
	proxy, hits := originAndProxy(t, handler)

	return &ImageProxy{
		client:      proxy.client,
		slots:       make(chan struct{}, imageFetchSlotCount),
		cache:       newIconCache(imageCacheBytes, imageCacheEntries),
		placeholder: proxy.placeholder,
	}, hits
}
