package crawlbroker

import "errors"

var errWorkerSessionActive = errors.New("crawl worker session is already connected")
