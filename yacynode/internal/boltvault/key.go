// Package boltvault owns the embedded database file and lends each module a
// transactionally-scoped, codec-typed collection over its own buckets. It is the
// single holder of the database handle; no caller receives the raw handle and no
// bolt type appears on its exported surface.
package boltvault

type Name string

type Key []byte

type Codec[V any] interface {
	Encode(V) ([]byte, error)
	Decode([]byte) (V, error)
}
