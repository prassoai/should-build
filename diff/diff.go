package diff

import (
	"fmt"
	"os/exec"
	"strings"
)

// Changed returns file paths changed between baseSHA and headSHA,
// relative to the repository root. Both arguments accept any git ref.
func Changed(repoRoot, baseRef, headRef string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", baseRef, headRef)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s %s: %w", baseRef, headRef, exitError(err))
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

func exitError(err error) error {
	if ee, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
