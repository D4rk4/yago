package yagocrawlcontract

import (
	"errors"
	"strings"
	"testing"
)

func TestHTTPValidatorsAcceptEveryHTTPValidatorForm(t *testing.T) {
	accepted := []HTTPValidators{
		{ETag: `"xyzzy"`},
		{ETag: `W/"xyzzy"`},
		{ETag: `""`},
		{ETag: `"!#$%&'()*+,-./0123456789:;<=>?@AZ[\]^_` + "`" + `az{|}~"`},
		{ETag: `"` + strings.Repeat("a", MaximumHTTPValidatorBytes-2) + `"`},
		{LastModified: "Sun, 06 Nov 1994 08:49:37 GMT"},
		{LastModified: "Sunday, 06-Nov-94 08:49:37 GMT"},
		{LastModified: "Sun Nov  6 08:49:37 1994"},
		{LastModified: "Sun Nov 13 08:49:37 1994"},
		{ETag: `W/"v1"`, LastModified: "Sun, 06 Nov 1994 08:49:37 GMT"},
	}
	for _, validators := range accepted {
		if err := validators.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want accepted", validators, err)
		}
	}
}

func TestHTTPValidatorsRefuseEmptyValue(t *testing.T) {
	if err := (HTTPValidators{}).Validate(); !errors.Is(err, errHTTPValidatorsEmpty) {
		t.Fatalf("Validate(empty) = %v, want %v", err, errHTTPValidatorsEmpty)
	}
}

func TestHTTPValidatorsRefuseOversizedEntityTag(t *testing.T) {
	oversized := HTTPValidators{
		ETag: `"` + strings.Repeat("a", MaximumHTTPValidatorBytes-1) + `"`,
	}
	if err := oversized.Validate(); !errors.Is(err, errEntityTagTooLarge) {
		t.Fatalf("Validate(%d-byte entity tag) = %v, want %v",
			len(oversized.ETag), err, errEntityTagTooLarge)
	}
}

func TestHTTPValidatorsRefuseInvalidEntityTagSyntax(t *testing.T) {
	refused := map[string]string{
		"lone quote":             `"`,
		"weak prefix only":       `W/`,
		"lowercase weak prefix":  `w/"xyzzy"`,
		"doubled weak prefix":    `W/W/"xyzzy"`,
		"missing opening quote":  `xyzzy"`,
		"missing closing quote":  `"xyzzy`,
		"unquoted":               `xyzzy`,
		"inner quote":            `"xy"zy"`,
		"inner space":            `"xy zy"`,
		"inner tab":              "\"xy\tzy\"",
		"inner carriage return":  "\"xy\rzy\"",
		"inner line feed":        "\"xy\nzy\"",
		"trailing line break":    "\"xyzzy\"\r\n",
		"inner delete":           "\"xy\x7fzy\"",
		"inner non-ASCII":        `"xyzzé"`,
		"inner invalid UTF-8":    "\"xy\xffzy\"",
		"header injection":       "\"a\"\r\nSet-Cookie: b",
		"surrounding whitespace": ` "xyzzy" `,
	}
	for name, entityTag := range refused {
		err := HTTPValidators{ETag: entityTag}.Validate()
		if !errors.Is(err, errEntityTagSyntax) {
			t.Errorf("%s: Validate(%q) = %v, want %v", name, entityTag, err, errEntityTagSyntax)
		}
	}
}

func TestHTTPValidatorsRefuseInvalidHTTPDateSyntax(t *testing.T) {
	refused := map[string]string{
		"wrong weekday":        "Mon, 06 Nov 1994 08:49:37 GMT",
		"single digit hour":    "Sun, 06 Nov 1994 8:49:37 GMT",
		"single digit day":     "Sun, 6 Nov 1994 08:49:37 GMT",
		"lowercase zone":       "Sun, 06 Nov 1994 08:49:37 gmt",
		"numeric zone":         "Sun, 06 Nov 1994 08:49:37 +0000",
		"other zone":           "Sun, 06 Nov 1994 08:49:37 UTC",
		"lowercase month":      "Sun, 06 nov 1994 08:49:37 GMT",
		"ISO 8601":             "1994-11-06T08:49:37Z",
		"trailing line break":  "Sun, 06 Nov 1994 08:49:37 GMT\r\n",
		"inner line feed":      "Sun, 06 Nov 1994\n08:49:37 GMT",
		"header injection":     "Sun, 06 Nov 1994 08:49:37 GMT\r\nSet-Cookie: b",
		"leading whitespace":   " Sun, 06 Nov 1994 08:49:37 GMT",
		"out of range day":     "Wed, 32 Nov 1994 08:49:37 GMT",
		"asctime wrong padded": "Sun Nov 06 08:49:37 1994",
		"oversized":            strings.Repeat("Sun, 06 Nov 1994 08:49:37 GMT", 64),
	}
	for name, lastModified := range refused {
		err := HTTPValidators{LastModified: lastModified}.Validate()
		if !errors.Is(err, errHTTPDateSyntax) {
			t.Errorf("%s: Validate(%q) = %v, want %v", name, lastModified, err, errHTTPDateSyntax)
		}
	}
}

func TestHTTPValidatorsRefuseOneInvalidValueBesideAValidOne(t *testing.T) {
	invalidEntityTag := HTTPValidators{
		ETag:         "\"a\"\r\n",
		LastModified: "Sun, 06 Nov 1994 08:49:37 GMT",
	}
	if err := invalidEntityTag.Validate(); !errors.Is(err, errEntityTagSyntax) {
		t.Fatalf("Validate(%+v) = %v, want %v", invalidEntityTag, err, errEntityTagSyntax)
	}
	invalidDate := HTTPValidators{ETag: `"a"`, LastModified: "yesterday"}
	if err := invalidDate.Validate(); !errors.Is(err, errHTTPDateSyntax) {
		t.Fatalf("Validate(%+v) = %v, want %v", invalidDate, err, errHTTPDateSyntax)
	}
}
