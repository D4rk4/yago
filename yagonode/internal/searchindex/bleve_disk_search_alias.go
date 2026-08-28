package searchindex

import "github.com/blevesearch/bleve/v2"

func newBleveDiskSearchAlias(shards []bleve.Index) bleve.Index {
	return bleve.NewIndexAlias(shards...)
}
