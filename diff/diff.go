package diff

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Changed returns file paths changed between base and head refs in the
// git repository rooted at repoRoot. Paths are relative to the repo root
// and use forward slashes.
func Changed(repoRoot, base, head string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", base, head)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("git diff: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("git diff: %w", err)
	}
	return parseNames(string(out)), nil
}

// parseNames splits git's newline-delimited output into file paths,
// trimming whitespace and dropping empty lines.
func parseNames(out string) []string {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}
