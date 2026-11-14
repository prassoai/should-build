package depgraph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Go analyzes Go source using "go list". Env is added to the go command's
// environment, for example GOOS=darwin to load another platform's files.
type Go struct {
	Env []string
}

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

// DepsAll loads the working tree at repoRoot once and returns each input
// pattern's transitive files. Any pattern or package that fails to load fails
// the whole call.
func (g Go) DepsAll(repoRoot string, importPaths []string) (map[string][]string, error) {
	return load(repoRoot, importPaths, false, g.Env...)
}

// DepsAt returns each input pattern's transitive files as of revision rev,
// read from the repository at repoRoot without touching its index, working
// tree or worktree list.
//
// The result is best effort: packages that fail to load at rev, including a
// pattern that does not exist there yet, contribute only what go list could
// resolve. It describes the files a target used to depend on, so a deleted
// file still selects its former consumers.
func (g Go) DepsAt(repoRoot, rev string, importPaths []string) (map[string][]string, error) {
	if len(importPaths) == 0 {
		return map[string][]string{}, nil
	}
	work, err := os.MkdirTemp("", "should-build-")
	if err != nil {
		return nil, fmt.Errorf("creating checkout directory for %s: %w", rev, err)
	}
	defer os.RemoveAll(work)
	tree := filepath.Join(work, "tree")
	if err := checkout(repoRoot, rev, filepath.Join(work, "index"), tree); err != nil {
		return nil, fmt.Errorf("checking out %s: %w", rev, err)
	}
	gowork, err := workspace(repoRoot, tree, g.Env)
	if err != nil {
		return nil, fmt.Errorf("mapping go workspace into %s: %w", rev, err)
	}
	return load(tree, importPaths, true, append(slices.Clone(g.Env), "GOWORK="+gowork)...)
}

// workspace returns the GOWORK value that gives the checkout at tree the same
// workspace the working tree at repoRoot uses under env. A go.work inside the repository
// maps to the same path in tree, copied there with its go.work.sum when the
// revision does not track it, so an ignored workspace file still joins the
// sibling modules. Inheriting GOWORK unchanged would instead load the working
// tree's modules. A workspace outside the repository cannot list the checkout,
// so it is turned off.
func workspace(repoRoot, tree string, env []string) (string, error) {
	cmd := exec.Command("go", "env", "GOWORK")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env GOWORK: %w", err)
	}
	gowork := strings.TrimSpace(string(out))
	switch gowork {
	case "", "off":
		return gowork, nil
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolving repo root: %w", err)
	}
	rel, err := filepath.Rel(absRoot, gowork)
	if err != nil || outsideRoot(rel) {
		return "off", nil
	}
	mapped := filepath.Join(tree, rel)
	if _, err := os.Stat(mapped); err == nil {
		return mapped, nil
	}
	for _, name := range []string{rel, rel + ".sum"} {
		data, err := os.ReadFile(filepath.Join(absRoot, name))
		if errors.Is(err, fs.ErrNotExist) && name != rel {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", name, err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(tree, name)), 0o755); err != nil {
			return "", fmt.Errorf("creating directory for %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(tree, name), data, 0o644); err != nil {
			return "", fmt.Errorf("copying %s: %w", name, err)
		}
	}
	return mapped, nil
}

// checkout writes rev's tree to dir through a private index file, so the
// repository's own index and working tree are left alone.
func checkout(repoRoot, rev, index, dir string) error {
	for _, args := range [][]string{
		{"read-tree", rev},
		{"checkout-index", "--all", "--prefix=" + dir + string(filepath.Separator)},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// load runs one go list over importPaths in repoRoot, with env added to the
// process environment. With tolerant set, go list reports broken packages
// instead of failing (-e).
func load(repoRoot string, importPaths []string, tolerant bool, env ...string) (map[string][]string, error) {
	results := make(map[string][]string, len(importPaths))
	if len(importPaths) == 0 {
		return results, nil
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving repo root: %w", err)
	}

	args := []string{"list", "-json", "-deps"}
	if tolerant {
		args = append(args, "-e")
	}
	cmd := exec.Command("go", append(args, importPaths...)...)
	cmd.Dir = absRoot
	cmd.Env = append(os.Environ(), env...)
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
