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

// goPackage is the subset of "go list -json" output we need.
type goPackage struct {
	Dir      string   `json:"Dir"`
	GoFiles  []string `json:"GoFiles"`
	Standard bool     `json:"Standard"`
}

// Deps returns all Go source files transitively imported by importPath,
// filtered to those under repoRoot. Standard library files are excluded.
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
		for _, f := range pkg.GoFiles {
			abs := filepath.Join(pkg.Dir, f)
			rel, err := filepath.Rel(absRoot, abs)
			if err != nil {
				continue
			}
			if outsideRoot(rel) {
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

