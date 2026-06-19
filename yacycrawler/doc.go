// Package yacycrawler is an optional, disposable crawl service that fetches URLs,
// builds YaCy-compatible RWI postings and URL metadata, and publishes them as
// ingest batches over a message-queue seam toward a YaCy RWI node.
//
// It never stores document bodies. Pipeline stages communicate only through the
// Publisher and Receiver seam (an in-process bounded queue here), so a real
// broker and the node's own consumer can replace the in-process pieces later.
//
// URL hashing in this package is provisional and not yet verified against the
// YaCy Java reference; word hashing reuses yacymodel and is the conformant path.
package yacycrawler
