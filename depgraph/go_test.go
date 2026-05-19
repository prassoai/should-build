package depgraph

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGoDepsDirect verifies that a target's own files are included in deps.
func TestGoDepsDirect(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(dir, "cmd", "app", "main.go"), "package main\n\nfunc main() {}\n")

	a := Go{}
	deps, err := a.Deps(dir, "./cmd/app")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cmd/app/main.go"}
	if !stringsEqual(deps, want) {
		t.Errorf("got %v, want %v", deps, want)
	}
}

// TestGoDepsTransitive verifies that transitive in-repo imports are included.
func TestGoDepsTransitive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(dir, "cmd", "app", "main.go"),
		"package main\n\nimport _ \"example.com/test/internal/lib\"\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "internal", "lib", "lib.go"), "package lib\n")

	a := Go{}
	deps, err := a.Deps(dir, "./cmd/app")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(deps)
	want := []string{"cmd/app/main.go", "internal/lib/lib.go"}
	if !stringsEqual(deps, want) {
		t.Errorf("got %v, want %v", deps, want)
	}
}

// TestGoDepsExcludesStdlib verifies that standard library files are not included.
func TestGoDepsExcludesStdlib(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }\n")

	a := Go{}
	deps, err := a.Deps(dir, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range deps {
		if d == "main.go" {
			continue
		}
		t.Errorf("unexpected dep (stdlib leak?): %s", d)
	}
}

// TestGoDepsInvalidPath verifies that a nonexistent import path returns an error.
func TestGoDepsInvalidPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.23\n")

	a := Go{}
	_, err := a.Deps(dir, "./nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent import path")
	}
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
