package match

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ExpandTarget replaces all occurrences of {target} in pattern with name.
func ExpandTarget(pattern, name string) string {
	return strings.ReplaceAll(pattern, "{target}", name)
}

// MatchAny reports whether path matches any of the patterns.
// On match it returns the pattern that matched.
func MatchAny(patterns []string, path string) (matched bool, pattern string, err error) {
	for _, p := range patterns {
		ok, err := doublestar.Match(p, path)
		if err != nil {
			return false, "", err
		}
		if ok {
			return true, p, nil
		}
	}
	return false, "", nil
}

// ValidatePattern checks whether pattern is a valid doublestar glob.
func ValidatePattern(pattern string) error {
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid glob pattern: %s", pattern)
	}
	return nil
}
