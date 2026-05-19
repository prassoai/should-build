package match

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Match reports whether path matches pattern using gitignore-style glob rules.
func Match(pattern, path string) (bool, error) {
	return doublestar.Match(pattern, path)
}

// ExpandTarget replaces all occurrences of {target} in pattern with name.
func ExpandTarget(pattern, name string) string {
	return strings.ReplaceAll(pattern, "{target}", name)
}

// MatchAny reports whether path matches any of the patterns.
// On match it returns the pattern that matched.
func MatchAny(patterns []string, path string) (matched bool, pattern string, err error) {
	for _, p := range patterns {
		ok, err := Match(p, path)
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
		return &PatternError{Pattern: pattern}
	}
	return nil
}

// PatternError records an invalid glob pattern.
type PatternError struct {
	Pattern string
}

func (e *PatternError) Error() string {
	return "invalid glob pattern: " + e.Pattern
}
