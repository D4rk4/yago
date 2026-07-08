package urlmetastaleness

type presenceCodec struct{}

func (presenceCodec) Encode(struct{}) ([]byte, error) { return []byte{}, nil }
func (presenceCodec) Decode([]byte) (struct{}, error) { return struct{}{}, nil }

type freshnessCodec struct{}

func (freshnessCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }
func (freshnessCodec) Decode(raw []byte) (string, error)   { return string(raw), nil }
