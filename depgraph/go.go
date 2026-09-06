package depgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
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
	ImportPath string   `json:"ImportPath"`
	Match      []string `json:"Match"`
	Deps       []string `json:"Deps"`
	Dir        string   `json:"Dir"`
	GoFiles    []string `json:"GoFiles"`
	EmbedFiles []string `json:"EmbedFiles"`
	Standard   bool     `json:"Standard"`
}

// Deps returns all Go source files and //go:embed assets transitively imported
// by importPath, filtered to those under repoRoot. Standard library files are
// excluded.
func (Go) Deps(repoRoot, importPath string) ([]string, error) {
	deps, err := (Go{}).DepsAll(repoRoot, []string{importPath})
	if err != nil {
		return nil, fmt.Errorf("loading dependencies for %s: %w", importPath, err)
	}
	return deps[importPath], nil
}

// DepsAll loads packages once and returns each input pattern's transitive files.
func (Go) DepsAll(repoRoot string, importPaths []string) (map[string][]string, error) {
	results := make(map[string][]string, len(importPaths))
	if len(importPaths) == 0 {
		return results, nil
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving repo root: %w", err)
	}

	cmd := exec.Command("go", append([]string{"list", "-json", "-deps"}, importPaths...)...)
	cmd.Dir = absRoot
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list %v: %s", importPaths, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("go list %v: %w", importPaths, err)
	}

	packages := make(map[string]goPackage)
	matched := make(map[string][]string)
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var pkg goPackage
		if err := dec.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		packages[pkg.ImportPath] = pkg
	}
	for _, root := range packages {
		if len(root.Match) == 0 {
			continue
		}
		var files []string
		for _, dependency := range append(root.Deps, root.ImportPath) {
			pkg, ok := packages[dependency]
			if !ok {
				return nil, fmt.Errorf("missing go list package %q", dependency)
			}
			if pkg.Standard || pkg.Dir == "" {
				continue
			}
			for _, file := range append(pkg.GoFiles, pkg.EmbedFiles...) {
				rel, err := filepath.Rel(absRoot, filepath.Join(pkg.Dir, file))
				if err != nil || outsideRoot(rel) {
					continue
				}
				files = append(files, filepath.ToSlash(rel))
			}
		}
		for _, pattern := range root.Match {
			matched[pattern] = append(matched[pattern], files...)
		}
	}
	for pattern, files := range matched {
		slices.Sort(files)
		matched[pattern] = slices.Compact(files)
	}
	for _, pattern := range importPaths {
		results[pattern] = matched[cleanPattern(pattern)]
	}
	return results, nil
}

func cleanPattern(pattern string) string {
	if filepath.IsAbs(pattern) {
		return filepath.Clean(pattern)
	}
	pattern = strings.ReplaceAll(pattern, `\`, "/")
	clean := path.Clean(pattern)
	if strings.HasPrefix(pattern, "./") && clean != "." {
		return "./" + clean
	}
	return clean
}

// outsideRoot reports whether a relative path escapes the root directory.
func outsideRoot(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
