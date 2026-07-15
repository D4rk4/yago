package vault

func (t *Txn) ReadBucketValue(name Name, key Key) ([]byte, bool) {
	raw := t.etx.Bucket(name).Get(key)
	if raw == nil {
		return nil, false
	}

	return append([]byte(nil), raw...), true
}
