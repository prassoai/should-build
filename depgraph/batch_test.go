package depgraph

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestGoDepsAllPreservesClosures requires one package load to retain each
// target's own transitive files, including overlapping patterns and embeds.
func TestGoDepsAllPreservesClosures(test *testing.T) {
	parent := test.TempDir()
	root := filepath.Join(parent, "repo")
	writeFile(test, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.23\n\nrequire example.com/external v0.0.0\nreplace example.com/external => ../repo-other\n")
	writeFile(test, filepath.Join(parent, "repo-other/go.mod"), "module example.com/external\n\ngo 1.23\n")
	writeFile(test, filepath.Join(parent, "repo-other/external.go"), "package external\n")
	writeFile(test, filepath.Join(root, "cmd/app/main.go"), "package main\nimport (_ \"example.com/test/internal/shared\"; _ \"example.com/external\")\nfunc main() {}\n")
	writeFile(test, filepath.Join(root, "cmd/other/main.go"), "package main\nimport (_ \"example.com/test/internal/shared\"; _ \"example.com/test/internal/private\")\nfunc main() {}\n")
	writeFile(test, filepath.Join(root, "cmd/isolated/main.go"), "package main\nimport \"fmt\"\nfunc main() { fmt.Println() }\n")
	writeFile(test, filepath.Join(root, "internal/shared/shared.go"), "package shared\nimport _ \"example.com/test/internal/leaf\"\n")
	writeFile(test, filepath.Join(root, "internal/private/private.go"), "package private\n")
	writeFile(test, filepath.Join(root, "internal/leaf/leaf.go"), "package leaf\nimport _ \"embed\"\n//go:embed data.txt\nvar Data string\n")
	writeFile(test, filepath.Join(root, "internal/leaf/data.txt"), "embedded\n")
	writeFile(test, filepath.Join(root, "internal/leaf/unused.txt"), "not embedded\n")

	realGo, err := exec.LookPath("go")
	if err != nil {
		test.Fatal(err)
	}
	bin := filepath.Join(parent, "bin")
	writeFile(test, filepath.Join(bin, "go"), "#!/bin/sh\nprintf 'call\\n' >> \"$GO_CALL_LOG\"\nexec \"$REAL_GO\" \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "go"), 0o755); err != nil {
		test.Fatal(err)
	}
	log := filepath.Join(parent, "calls")
	test.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	test.Setenv("REAL_GO", realGo)
	test.Setenv("GO_CALL_LOG", log)
	patterns := []string{"./cmd/app", "./cmd/other", "./cmd/isolated", "./internal/shared", "./cmd/...", "example.com/test/cmd/app", "./cmd/app/", "./cmd/../cmd/app", "./cmd/app"}
	results, err := (Go{}).DepsAll(root, patterns)
	if err != nil {
		test.Fatal(err)
	}
	calls, err := os.ReadFile(log)
	if err != nil || string(calls) != "call\n" {
		test.Fatalf("package loads = %q, error = %v; want one", calls, err)
	}
	for _, pattern := range patterns {
		command := exec.Command(realGo, "list", "-json", "-deps", pattern)
		command.Dir = root
		output, err := command.Output()
		if err != nil {
			test.Fatal(err)
		}
		var expected []string
		decoder := json.NewDecoder(bytes.NewReader(output))
		for decoder.More() {
			var pkg goPackage
			if err := decoder.Decode(&pkg); err != nil {
				test.Fatal(err)
			}
			if pkg.Standard {
				continue
			}
			for _, file := range append(pkg.GoFiles, pkg.EmbedFiles...) {
				relative, err := filepath.Rel(root, filepath.Join(pkg.Dir, file))
				if err == nil && !outsideRoot(relative) {
					expected = append(expected, filepath.ToSlash(relative))
				}
			}
		}
		slices.Sort(expected)
		if !slices.Equal(results[pattern], slices.Compact(expected)) {
			test.Errorf("%s: got %v, want %v", pattern, results[pattern], expected)
		}
	}
	if slices.Contains(results["./cmd/app"], "internal/private/private.go") ||
		!slices.Contains(results["./cmd/app"], "internal/leaf/data.txt") {
		test.Fatal("target isolation and transitive embeds must be preserved")
	}
}

// TestGoDepsAllEmpty requires pattern-only configurations to avoid invoking Go.
func TestGoDepsAllEmpty(test *testing.T) {
	test.Setenv("PATH", test.TempDir())
	results, err := (Go{}).DepsAll(test.TempDir(), nil)
	if err != nil || len(results) != 0 {
		test.Fatalf("got %v, %v; want an empty result without Go", results, err)
	}
}

// TestGoDepsAllInvalid requires one invalid target to fail the complete load.
func TestGoDepsAllInvalid(test *testing.T) {
	root := test.TempDir()
	writeFile(test, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(test, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	_, err := (Go{}).DepsAll(root, []string{".", "./missing"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		test.Fatalf("want a missing-target error, got %v", err)
	}
}
