package peerprofile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

func profileSourceBytes(size int) []byte {
	prefix := []byte("operator=alice\n#")
	source := make([]byte, size)
	copy(source, prefix)
	for index := len(prefix); index < len(source); index++ {
		source[index] = 'x'
	}

	return source
}

func TestProfileSourceSizeExactAndPlusOne(t *testing.T) {
	exact := ProfileFile{files: fstest.MapFS{
		profileFileName: {Data: profileSourceBytes(maximumProfileSourceBytes)},
	}}
	properties := exact.Properties(t.Context())
	if len(properties) != 1 || properties[0] != (Property{Key: "operator", Value: "alice"}) {
		t.Fatalf("exact properties = %v", properties)
	}
	oversized := ProfileFile{files: fstest.MapFS{
		profileFileName: {Data: profileSourceBytes(maximumProfileSourceBytes + 1)},
	}}
	if properties := oversized.Properties(t.Context()); len(properties) != 0 {
		t.Fatalf("plus-one properties = %v", properties)
	}
}

func profileProperties(count int) string {
	var source strings.Builder
	for index := range count {
		fmt.Fprintf(&source, "key%d=value\n", index)
	}

	return source.String()
}

func TestProfilePropertyCountExactAndPlusOne(t *testing.T) {
	exact, err := parseProfileProperties(
		t.Context(),
		profileProperties(maximumProfileProperties),
	)
	if err != nil || len(exact) != maximumProfileProperties {
		t.Fatalf("exact properties=%d error=%v", len(exact), err)
	}
	plusOne, err := parseProfileProperties(
		t.Context(),
		profileProperties(maximumProfileProperties+1),
	)
	if !errors.Is(err, errProfilePropertiesTooLarge) || plusOne != nil {
		t.Fatalf("plus-one properties=%v error=%v", plusOne, err)
	}
	profile := ProfileFile{files: fstest.MapFS{
		profileFileName: {Data: []byte(profileProperties(maximumProfileProperties + 1))},
	}}
	if properties := profile.Properties(t.Context()); properties != nil {
		t.Fatalf("file plus-one properties = %v", properties)
	}
}

func TestProfilePropertyBytesExactAndPlusOne(t *testing.T) {
	exactValue := strings.Repeat("v", maximumProfilePropertyBytes-1)
	exact, err := parseProfileProperties(t.Context(), "k="+exactValue)
	if err != nil || len(exact) != 1 || len(exact[0].Key)+len(exact[0].Value) !=
		maximumProfilePropertyBytes {
		t.Fatalf("exact properties=%d error=%v", len(exact), err)
	}
	plusOne, err := parseProfileProperties(t.Context(), "k="+exactValue+"v")
	if !errors.Is(err, errProfilePropertiesTooLarge) || plusOne != nil {
		t.Fatalf("plus-one properties=%v error=%v", plusOne, err)
	}
}

func retainedProfileStringAddress(value string) uintptr {
	return uintptr(reflect.ValueOf(value).UnsafePointer())
}

func TestProfilePropertyBudgetOwnsRetainedStrings(t *testing.T) {
	backing := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 1<<16)
	key := backing[100:120]
	value := backing[200:240]
	property, retained := (&profilePropertyBudget{}).retain(key, value)
	keyDetached := retainedProfileStringAddress(property.Key) != retainedProfileStringAddress(key)
	valueDetached := retainedProfileStringAddress(property.Value) !=
		retainedProfileStringAddress(value)
	if !retained || !keyDetached || !valueDetached {
		t.Fatal("profile property retained source backing storage")
	}
}

func TestProfileResponseSizeExactAndPlusOne(t *testing.T) {
	exactValue := strings.Repeat(
		"v",
		maximumProfileResponseBytes-len("k=")-len(profileLineBreak),
	)
	exact := encodeProperties([]Property{{Key: "k", Value: exactValue}})
	if len(exact) != maximumProfileResponseBytes {
		t.Fatalf("exact response bytes = %d", len(exact))
	}
	plusOne := encodeProperties([]Property{{Key: "k", Value: exactValue + "v"}})
	if plusOne != "" {
		t.Fatalf("plus-one response bytes = %d", len(plusOne))
	}
	tooMany := make([]Property, maximumProfileProperties+1)
	for index := range tooMany {
		tooMany[index] = Property{Key: "k", Value: "v"}
	}
	if encoded := encodeProperties(tooMany); encoded != "" {
		t.Fatalf("too-many response bytes = %d", len(encoded))
	}
	if size := sanitizedProfilePartSize(
		strings.Repeat("x", maximumProfileResponseBytes+1),
		maximumProfileResponseBytes,
	); size != maximumProfileResponseBytes+1 {
		t.Fatalf("oversized sanitized bytes = %d", size)
	}
}

type staticProfile []Property

func (p staticProfile) Properties(context.Context) []Property { return p }

func TestProfileEndpointReturnsEmptySuccessOnResponseOverflow(t *testing.T) {
	value := strings.Repeat("v", maximumProfileResponseBytes)
	response, err := (endpoint{
		identity: localIdentity(),
		profile:  staticProfile{{Key: "k", Value: value}},
	}).Serve(t.Context(), yagoproto.ProfileRequest{NetworkName: "freeworld"})
	if err != nil || response.ContentType != profileContentType || response.Body != "" {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}

type unreadProfileBody struct {
	read bool
}

func (b *unreadProfileBody) Read([]byte) (int, error) {
	b.read = true

	return 0, io.EOF
}

func (*unreadProfileBody) Close() error { return nil }

func TestProfileAdmissionShedsBeforeParsingAndRecovers(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	admission := httpguard.NewIntakeGate(maximumConcurrentProfileRequests)
	releases := make([]func(), 0, maximumConcurrentProfileRequests)
	for range maximumConcurrentProfileRequests {
		release, admitted := admission.TryAcquire()
		if !admitted {
			t.Fatalf("profile admission stopped at %d", len(releases))
		}
		releases = append(releases, release)
	}
	profile := &recordingProfile{}
	mountWithAdmission(router, localIdentity(), profile, admission)
	body := &unreadProfileBody{}
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, yagoproto.PathProfile, body,
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	result := httptest.NewRecorder()
	mux.ServeHTTP(result, request)
	if result.Code != http.StatusServiceUnavailable ||
		result.Header().Get("Retry-After") != "1" || body.read || profile.consulted {
		t.Fatalf(
			"result=%d retry=%q read=%t consulted=%t",
			result.Code,
			result.Header().Get("Retry-After"),
			body.read,
			profile.consulted,
		)
	}
	for _, release := range releases {
		release()
	}
	form := yagoproto.ProfileRequest{NetworkName: "freeworld"}.Form()
	result = httptest.NewRecorder()
	mux.ServeHTTP(result, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, yagoproto.PathProfile+"?"+form.Encode(), nil,
	))
	if result.Code != http.StatusOK || !profile.consulted ||
		!strings.Contains(result.Body.String(), "nickname=Yago") {
		t.Fatalf(
			"recovery=%d consulted=%t body=%q",
			result.Code,
			profile.consulted,
			result.Body.String(),
		)
	}
	finalReleases := make([]func(), 0, maximumConcurrentProfileRequests)
	for range maximumConcurrentProfileRequests {
		finalRelease, admitted := admission.TryAcquire()
		if !admitted {
			t.Fatalf("successful profile retained admission at %d", len(finalReleases))
		}
		finalReleases = append(finalReleases, finalRelease)
	}
	for _, finalRelease := range finalReleases {
		finalRelease()
	}
}

type profileReadSpy struct {
	reads int
}

func (r *profileReadSpy) Read([]byte) (int, error) {
	r.reads++

	return 0, io.EOF
}

func TestProfileContextReaderStopsBeforeSourceRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	source := &profileReadSpy{}
	_, err := (profileContextReader{ctx: ctx, source: source}).Read(make([]byte, 1))
	if !errors.Is(err, context.Canceled) || source.reads != 0 {
		t.Fatalf("error=%v reads=%d", err, source.reads)
	}
	properties, err := parseProfileProperties(ctx, "key=value")
	if !errors.Is(err, context.Canceled) || properties != nil {
		t.Fatalf("parse properties=%v error=%v", properties, err)
	}
	if _, err := readProfileSource(ctx, fstest.MapFS{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("source error = %v", err)
	}
	properties, err = parseProfileProperties(ctx, "")
	if !errors.Is(err, context.Canceled) || properties != nil {
		t.Fatalf("empty parse properties=%v error=%v", properties, err)
	}
}

type cancelingProfileFS struct {
	cancel context.CancelFunc
}

func (f cancelingProfileFS) Open(name string) (fs.File, error) {
	file, err := (fstest.MapFS{
		profileFileName: {Data: []byte("key=value")},
	}).Open(name)
	if err != nil {
		return nil, fmt.Errorf("open canceling profile: %w", err)
	}

	return &cancelingProfileFile{File: file, cancel: f.cancel}, nil
}

type cancelingProfileFile struct {
	fs.File
	cancel context.CancelFunc
	once   sync.Once
}

func (f *cancelingProfileFile) Read(destination []byte) (int, error) {
	read, _ := f.File.Read(destination)
	f.once.Do(f.cancel)

	return read, io.EOF
}

func TestProfileSourceReturnsContextCancellationAfterRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	files := cancelingProfileFS{cancel: cancel}
	_, err := readProfileSource(ctx, files)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	profileContext, profileCancel := context.WithCancel(t.Context())
	defer profileCancel()
	files = cancelingProfileFS{cancel: profileCancel}
	if properties := (ProfileFile{files: files}).Properties(profileContext); properties != nil {
		t.Fatalf("canceled file properties = %v", properties)
	}
}

type stagedProfileContext struct {
	context.Context
	calls    int
	cancelAt int
}

func (c *stagedProfileContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}

	return nil
}

type singleReadProfileFS struct{}

func (singleReadProfileFS) Open(name string) (fs.File, error) {
	file, err := (fstest.MapFS{
		profileFileName: {Data: []byte("key=value")},
	}).Open(name)
	if err != nil {
		return nil, fmt.Errorf("open single-read profile: %w", err)
	}

	return &singleReadProfileFile{File: file}, nil
}

type singleReadProfileFile struct {
	fs.File
}

func (f *singleReadProfileFile) Read(destination []byte) (int, error) {
	read, _ := f.File.Read(destination)

	return read, io.EOF
}

func TestProfileFileReturnsEmptyWhenContextCancelsBeforeParse(t *testing.T) {
	ctx := &stagedProfileContext{Context: t.Context(), cancelAt: 5}
	properties := (ProfileFile{files: singleReadProfileFS{}}).Properties(ctx)
	if properties != nil || ctx.calls != ctx.cancelAt {
		t.Fatalf("properties=%v context calls=%d", properties, ctx.calls)
	}
}
