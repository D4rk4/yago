// Package yacycrawler is an optional, disposable crawl service that fetches URLs,
// builds YaCy-compatible RWI postings and URL metadata, and publishes them as
// ingest batches over a message-queue seam toward a YaCy RWI node.
//
// It never stores document bodies. Pipeline stages communicate only through the
// Publisher and Receiver seam (an in-process bounded queue here), so a real
// broker and the node's own consumer can replace the in-process pieces later.
//
// URL and word hashing both delegate to yacymodel, which reproduces the YaCy
// Java reference algorithms.
package yacycrawler
