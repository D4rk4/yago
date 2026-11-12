package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

func pageObservationIdentity(pageIdentity string, fetchedAt time.Time) string {
	identity := sha256.Sum256([]byte(pageIdentity + "\x00" + fetchedAt.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(identity[:])
}
