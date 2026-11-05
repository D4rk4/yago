// Package peerwire encodes outbound peer-protocol POST bodies.
package peerwire

import (
	"bytes"
	"mime/multipart"
	"net/url"
)

func EncodeMultipartForm(fields url.Values) ([]byte, string) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, values := range fields {
		for _, value := range values {
			_ = writer.WriteField(name, value)
		}
	}
	_ = writer.Close()

	return body.Bytes(), writer.FormDataContentType()
}
