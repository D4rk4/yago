package yagomodel

func EncodeSeedWireForm(seed Seed) string {
	payload := seed.String()
	base64 := EncodeBase64WireForm(payload)
	compressed := tagged(wireFormGzip, Encode(gzipCompress(payload)))
	if len(base64) < len(compressed) {
		return base64
	}

	return compressed
}
