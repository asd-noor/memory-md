// Package pathenc encodes MEMORY_MD_DIR into a cache-directory name.
//
// Encoding: strip the leading '/', replace all remaining '/' with '='.
//
//	/home/user/notes  →  home=user=notes
//	/projects/foo     →  projects=foo
package pathenc

import "strings"

// Encode converts an absolute directory path to a filesystem-safe cache name.
func Encode(dir string) string {
	s := strings.TrimPrefix(dir, "/")
	return strings.ReplaceAll(s, "/", "=")
}
