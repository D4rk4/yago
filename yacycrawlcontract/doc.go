// Package yacycrawlcontract defines the message types exchanged between a YaCy
// RWI node and a disposable crawl service. The node hands crawl work down as
// CrawlOrder values and receives results up as IngestBatch values; both ends
// import this package so neither service depends on the other.
//
// The contract is deliberately free of the YaCy HTTP wire format: it carries
// only the crawl parameters meaningful to a references-only crawler. Provenance
// is an opaque, node-owned token that the crawler echoes back without
// inspecting, so admin-initiated and remote crawl orders share one seam.
package yacycrawlcontract
