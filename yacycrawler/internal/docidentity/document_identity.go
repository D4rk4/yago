package docidentity

import (
	"crypto/sha256"
	"encoding/hex"
)

func DocumentID(canonicalURL string) string {
	sum := sha256.Sum256([]byte(canonicalURL))
	return hex.EncodeToString(sum[:])
}
