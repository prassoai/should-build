package match

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Expand replaces {target} in pattern with the given target name.
func Expand(pattern, target string) string {
	return strings.ReplaceAll(pattern, "{target}", target)
}

// ExpandAll replaces {target} in every pattern.
func ExpandAll(patterns []string, target string) []string {
	out := make([]string, len(patterns))
	for i, p := range patterns {
		out[i] = Expand(p, target)
	}
	return out
}

// File reports whether path matches the glob pattern.
// Patterns use doublestar syntax: * matches within a segment,
// ** matches across segments, {a,b} is alternation.
func File(pattern, path string) bool {
	ok, _ := doublestar.Match(pattern, path)
	return ok
}

// Any reports whether path matches any of the patterns.
// Returns the first matched pattern and true, or "" and false.
func Any(path string, patterns []string) (string, bool) {
	for _, p := range patterns {
		if File(p, path) {
			return p, true
		}
	}
	return "", false
}
