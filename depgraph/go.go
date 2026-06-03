package depgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Go implements Analyzer for Go source using "go list".
type Go struct{}

// goPackage is the subset of "go list -json" output we need. EmbedFiles holds
// files pulled in by //go:embed directives (paths relative to Dir, like
// GoFiles); without them a change to an embedded asset — e.g. a VERSION file
// compiled into a binary — would be invisible to the dep graph and fall through
// to the trigger_all / unknown_file fallback, rebuilding every target.
type goPackage struct {
	Dir        string   `json:"Dir"`
	GoFiles    []string `json:"GoFiles"`
	EmbedFiles []string `json:"EmbedFiles"`
	Standard   bool     `json:"Standard"`
}

// Deps returns all Go source files and //go:embed assets transitively imported
// by importPath, filtered to those under repoRoot. Standard library files are
// excluded.
func (Go) Deps(repoRoot, importPath string) ([]string, error) {
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving repo root: %w", err)
	}

	cmd := exec.Command("go", "list", "-json", "-deps", importPath)
	cmd.Dir = absRoot
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list %s: %s", importPath, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("go list %s: %w", importPath, err)
	}

	var files []string
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var pkg goPackage
		if err := dec.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		if pkg.Standard || pkg.Dir == "" {
			continue
		}
		for _, f := range append(pkg.GoFiles, pkg.EmbedFiles...) {
			rel, err := filepath.Rel(absRoot, filepath.Join(pkg.Dir, f))
			if err != nil || outsideRoot(rel) {
				continue
			}
			files = append(files, filepath.ToSlash(rel))
		}
	}
	return files, nil
}

// outsideRoot reports whether a relative path escapes the root directory.
func outsideRoot(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
