package depgraph

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// git runs git in dir isolated from the host's global config and returns its
// trimmed output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestGoDepsAtSeesDeletedFiles requires the graph of an earlier revision to
// list a file the working tree has since deleted. The head graph cannot know
// which targets used a deleted file, so without the base graph its deletion
// selects nothing, or falls through to the unknown_file policy.
func TestGoDepsAtSeesDeletedFiles(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@test.com")
	git(t, root, "config", "user.name", "Test")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(root, "cmd/app/main.go"), "package main\nimport _ \"example.com/test/lib\"\nfunc main() {}\n")
	writeFile(t, filepath.Join(root, "lib/foo.go"), "package lib\n")
	writeFile(t, filepath.Join(root, "lib/bar.go"), "package lib\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")
	base := git(t, root, "rev-parse", "HEAD")
	git(t, root, "rm", "-q", "lib/foo.go")
	git(t, root, "commit", "-m", "head")

	head, err := (Go{}).DepsAll(root, []string{"./cmd/app"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := (Go{}).DepsAt(root, base, []string{"./cmd/app"})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(head["./cmd/app"], "lib/foo.go") {
		t.Fatalf("head graph %v lists the deleted file", head["./cmd/app"])
	}
	if want := []string{"cmd/app/main.go", "lib/bar.go", "lib/foo.go"}; !slices.Equal(before["./cmd/app"], want) {
		t.Fatalf("base graph = %v, want %v", before["./cmd/app"], want)
	}
	if status := git(t, root, "status", "--porcelain"); status != "" {
		t.Fatalf("reading the base revision changed the repository:\n%s", status)
	}
}

// TestGoDepsAtNewTarget requires a target that does not exist yet at the
// revision to contribute no files rather than fail: a new command has no
// former consumers, and its added files are all in the head graph.
func TestGoDepsAtNewTarget(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@test.com")
	git(t, root, "config", "user.name", "Test")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "base")

	graphs, err := (Go{}).DepsAt(root, "HEAD", []string{".", "./cmd/new", "./cmd/new/..."})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(graphs["."], []string{"main.go"}) || len(graphs["./cmd/new"]) != 0 || len(graphs["./cmd/new/..."]) != 0 {
		t.Fatalf("got %v; want only main.go for the existing root", graphs)
	}
}

// TestGoDepsAtUnknownRevision requires an unresolvable revision to fail
// rather than silently produce an empty graph.
func TestGoDepsAtUnknownRevision(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	if _, err := (Go{}).DepsAt(root, "no-such-rev", []string{"."}); err == nil || !strings.Contains(err.Error(), "no-such-rev") {
		t.Fatalf("want an error naming the revision, got %v", err)
	}
}
