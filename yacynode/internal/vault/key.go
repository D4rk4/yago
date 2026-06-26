// Package vault lends each module a transactionally-scoped, codec-typed
// collection over its own ordered byte buckets. It defines the storage seam: an
// Engine driver contract that a storage medium implements, and the
// Collection/Txn surface the rest of the node speaks. No storage-medium type
// appears on its exported surface.
package vault

type Name string

type Key []byte

type Codec[V any] interface {
	Encode(V) ([]byte, error)
	Decode([]byte) (V, error)
}
