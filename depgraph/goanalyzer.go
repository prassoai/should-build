package depgraph

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Compile-time assertion that Go implements Analyzer.
var _ Analyzer = Go{}

// Go implements [Analyzer] for Go modules using go/packages.
type Go struct{}

// Deps returns the Go source files (non-test) transitively imported by
// the package at importPath, including the package's own files.
// Only files under repoRoot are returned; standard library and external
// module files are excluded.
func (Go) Deps(repoRoot, importPath string) ([]string, error) {
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving repo root: %w", err)
	}

	cfg := &packages.Config{
		Dir:  absRoot,
		Mode: packages.NeedFiles | packages.NeedImports | packages.NeedDeps,
	}
	pkgs, err := packages.Load(cfg, importPath)
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found for %s", importPath)
	}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("package %s: %s", pkg.PkgPath, pkg.Errors[0])
		}
	}

	var files []string
	var walkErr error
	visited := make(map[string]bool)
	var walk func(*packages.Package)
	walk = func(pkg *packages.Package) {
		if visited[pkg.ID] || walkErr != nil {
			return
		}
		visited[pkg.ID] = true

		// Propagate errors from transitive dependencies.
		if len(pkg.Errors) > 0 {
			walkErr = fmt.Errorf("package %s: %s", pkg.PkgPath, pkg.Errors[0])
			return
		}

		for _, f := range pkg.GoFiles {
			rel, err := filepath.Rel(absRoot, f)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue // outside repo root (stdlib, external module)
			}
			files = append(files, filepath.ToSlash(rel))
		}
		for _, imp := range pkg.Imports {
			walk(imp)
		}
	}
	for _, pkg := range pkgs {
		walk(pkg)
	}
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}
