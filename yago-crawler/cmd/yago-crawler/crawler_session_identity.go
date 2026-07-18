package main

import (
	"crypto/rand"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func newCrawlerSessionID(workerID string) string {
	for {
		sessionID := rand.Text()
		if sessionID != workerID && yagocrawlcontract.ValidCrawlerSessionIdentity(sessionID) {
			return sessionID
		}
	}
}
