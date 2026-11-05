package peerwire

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestEncodeMultipartFormRoundTripsEveryValue(t *testing.T) {
	fields := url.Values{
		"empty":   {""},
		"index":   {"0", "1", "2"},
		"unicode": {"café — résumé — 日本語 — 😀"},
		"newline": {"line-one\nline-two\r\nline-three\n"},
		"url0":    {"{descr=YWJj+/=,hash=AAAAAAAAAAAA,url=aHR0cHM6Ly9leGFtcGxlLm9yZy8%3D}"},
	}
	body, contentType := EncodeMultipartForm(fields)

	if got := decodeMultipartForm(t, body, contentType); !reflect.DeepEqual(got, fields) {
		t.Fatalf("decoded form = %q, want %q", got, fields)
	}
	if !bytes.Contains(body, []byte(fields.Get("url0"))) {
		t.Fatal("field value was not written verbatim")
	}
}

func TestEncodeMultipartFormKeepsNetworkAuthenticationValid(t *testing.T) {
	access := yagoproto.NetworkAccess{
		NetworkName: "private",
		Mode:        yagoproto.NetworkAuthenticationSaltedMagic,
		Essentials:  "synthetic-network-essentials",
		Self:        yagomodel.WordHash("synthetic-self"),
	}
	fields := url.Values{
		yagoproto.FieldNetworkName: {access.NetworkName},
		yagoproto.FieldIam:         {access.Self.String()},
		yagoproto.FieldYouAre:      {yagomodel.WordHash("synthetic-peer").String()},
	}
	if err := access.Sign(fields); err != nil {
		t.Fatalf("sign form: %v", err)
	}
	body, contentType := EncodeMultipartForm(fields)
	decoded := decodeMultipartForm(t, body, contentType)

	if !reflect.DeepEqual(decoded, fields) || !access.Authorizes(decoded) {
		t.Fatalf("decoded authentication fields = %q, want authorized %q", decoded, fields)
	}
	tampered := url.Values{}
	for name, values := range decoded {
		tampered[name] = append([]string(nil), values...)
	}
	tampered.Set(yagoproto.FieldIam, yagomodel.WordHash("synthetic-other").String())
	if access.Authorizes(tampered) {
		t.Fatal("authentication accepted a form whose sender changed after signing")
	}
}

func TestEncodeMultipartFormEncodesEmptyFormAsTerminatedMultipart(t *testing.T) {
	body, contentType := EncodeMultipartForm(url.Values{})
	boundary := multipartBoundary(t, contentType)

	if string(body) != "\r\n--"+boundary+"--\r\n" {
		t.Fatalf("empty form body = %q", body)
	}
	if got := decodeMultipartForm(t, body, contentType); len(got) != 0 {
		t.Fatalf("decoded empty form = %q", got)
	}
}

func TestEncodeMultipartFormDelimitsBodyWithContentTypeBoundary(t *testing.T) {
	body, contentType := EncodeMultipartForm(url.Values{"count": {"1"}})
	boundary := multipartBoundary(t, contentType)

	if !bytes.HasPrefix(body, []byte("--"+boundary+"\r\n")) {
		t.Fatalf("body does not open with the content type boundary: %q", body)
	}
	if !bytes.HasSuffix(body, []byte("\r\n--"+boundary+"--\r\n")) {
		t.Fatalf("body does not close with the final boundary: %q", body)
	}
	if _, err := multipart.NewReader(bytes.NewReader(body), boundary+"x").NextPart(); err == nil {
		t.Fatal("body parsed with a boundary other than the advertised one")
	}
}

func TestEncodeMultipartFormCarriesValuesAboveJavaFormLimit(t *testing.T) {
	fields := url.Values{"descr": {strings.Repeat("x", 210000)}}
	body, contentType := EncodeMultipartForm(fields)

	if len(body) <= 200000 {
		t.Fatalf("body length = %d, want above 200000", len(body))
	}
	if got := decodeMultipartForm(t, body, contentType); !reflect.DeepEqual(got, fields) {
		t.Fatalf("large value changed: decoded %d bytes", len(got.Get("descr")))
	}
}

func multipartBoundary(t *testing.T, contentType string) string {
	t.Helper()
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		t.Fatalf("content type = %q, error = %v", contentType, err)
	}

	return parameters["boundary"]
}

func decodeMultipartForm(t *testing.T, body []byte, contentType string) url.Values {
	t.Helper()
	reader := multipart.NewReader(bytes.NewReader(body), multipartBoundary(t, contentType))
	decoded := url.Values{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return decoded
		}
		if err != nil {
			t.Fatalf("read multipart part: %v", err)
		}
		value, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read multipart value: %v", err)
		}
		decoded.Add(part.FormName(), string(value))
	}
}
