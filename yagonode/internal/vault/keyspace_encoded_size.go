package vault

func (k *Keyspace[V]) EncodedSize(tx *Txn, key Key) (int, bool, error) {
	return encodedValueSize(tx, k.name, key)
}
