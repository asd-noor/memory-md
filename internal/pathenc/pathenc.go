// Package pathenc encodes MEMORY_MD_DIR into a cache-directory name.
//
// Encoding: first 16 hex characters of the SHA-256 digest of the absolute path.
// This produces a fixed-length, filesystem-safe name regardless of how long or
// deeply nested MEMORY_MD_DIR is, keeping Unix socket paths well under the
// 104-byte sun_path limit on macOS (and 108-byte limit on Linux).
//
//	/home/user/notes  →  e.g. "3b4c1a9f2d8e07c5"
//	/projects/foo     →  e.g. "a17f3c820e9d1b64"
package pathenc

import (
	"crypto/sha256"
	"encoding/hex"
)

// Encode converts an absolute directory path to a fixed-length cache dir name
// (first 16 hex chars of SHA-256). The name is always 16 characters long.
func Encode(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return hex.EncodeToString(sum[:])[:16]
}
