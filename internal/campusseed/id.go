package campusseed

import (
	"crypto/sha256"
	"encoding/hex"
)

const idNamespace = "campus-demo-v1"

// stableID returns a deterministic 32-character hex identifier for demo data.
func stableID(kind, slug string) string {
	digest := sha256.Sum256([]byte(idNamespace + "\x00" + kind + "\x00" + slug))
	return hex.EncodeToString(digest[:16])
}
