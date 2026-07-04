package urlreferences

type presenceCodec struct{}

func (presenceCodec) Encode(struct{}) ([]byte, error) { return []byte{}, nil }

func (presenceCodec) Decode([]byte) (struct{}, error) { return struct{}{}, nil }
