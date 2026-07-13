package main

import (
	"crypto/rand"
	"strings"
)

func instanceWorkerID(configured string) string {
	prefix := strings.TrimSpace(configured)
	if prefix == "" {
		prefix = DefaultWorkerID
	}

	return prefix + "-" + rand.Text()
}
