package diff

import (
	"fmt"
	"os/exec"
	"strings"
)

// Changed returns file paths changed between baseRef and headRef,
// relative to the repository root. Both arguments accept any git ref.
//
// Rename detection is disabled (--no-renames) so both the old and new
// paths appear in the output, ensuring the old target rebuilds too.
// Quoted-path escaping is disabled (-c core.quotepath=false) so
// non-ASCII filenames are returned verbatim.
func Changed(repoRoot, baseRef, headRef string) ([]string, error) {
	cmd := exec.Command("git",
		"-c", "core.quotepath=false",
		"diff", "--no-renames", "--name-only",
		baseRef, headRef,
	)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git diff %s %s: %s", baseRef, headRef, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("git diff %s %s: %w", baseRef, headRef, err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}
