package depgraph

import (
	"sort"
	"testing"
)

// TestGoAnalyzerTransitiveDeps verifies that the Go analyzer returns
// all source files transitively imported by a target, including the
// target's own files. The test uses a self-contained Go module in testdata/.
func TestGoAnalyzerTransitiveDeps(t *testing.T) {
	var a Go
	got, err := a.Deps("testdata/gomod", "./cmd/server")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{
		"cmd/server/main.go",
		"internal/config/config.go",
		"internal/util/util.go",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d files %v, want %d files %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestGoAnalyzerExcludesStdlib verifies that standard library files
// are not included in the output.
func TestGoAnalyzerExcludesStdlib(t *testing.T) {
	var a Go
	got, err := a.Deps("testdata/gomod", "./cmd/server")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		// stdlib files would have paths outside testdata/gomod,
		// which filepath.Rel would prefix with ".."
		if f == "" || f[0] == '.' {
			t.Errorf("unexpected file path: %q (should not include stdlib)", f)
		}
	}
}

// TestGoAnalyzerLeafPackage verifies that a leaf package with no
// in-repo transitive deps returns only its own files.
func TestGoAnalyzerLeafPackage(t *testing.T) {
	var a Go
	got, err := a.Deps("testdata/gomod", "./internal/util")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "internal/util/util.go" {
		t.Errorf("got %v, want [internal/util/util.go]", got)
	}
}

// TestGoAnalyzerInvalidPath verifies that a nonexistent import path
// returns an error.
func TestGoAnalyzerInvalidPath(t *testing.T) {
	var a Go
	_, err := a.Deps("testdata/gomod", "./cmd/nonexistent")
	if err == nil {
		t.Error("Deps with nonexistent path should fail")
	}
}
