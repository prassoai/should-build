package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// gitEnv returns env vars that isolate git from the host's global config.
// This prevents test failures on machines with commit signing, custom
// hooks, or non-default init templates.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
}

// run executes a command in dir and fails the test on error.
func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

// initRepo creates a fresh git repo in a temp dir with an initial commit.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, ".gitkeep"), "")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial")
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1]) // trim newline
}

// TestChangedAddFile verifies detection of a newly added file.
func TestChangedAddFile(t *testing.T) {
	dir := initRepo(t)
	base := sha(t, dir, "HEAD")

	writeFile(t, filepath.Join(dir, "new.go"), "package main")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "add file")
	head := sha(t, dir, "HEAD")

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"new.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestChangedDeleteFile verifies detection of a deleted file.
func TestChangedDeleteFile(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "doomed.go"), "package main")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "add doomed")
	base := sha(t, dir, "HEAD")

	os.Remove(filepath.Join(dir, "doomed.go"))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "delete doomed")
	head := sha(t, dir, "HEAD")

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"doomed.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestChangedSubdirectory verifies paths include subdirectory prefixes.
func TestChangedSubdirectory(t *testing.T) {
	dir := initRepo(t)
	base := sha(t, dir, "HEAD")

	writeFile(t, filepath.Join(dir, "a", "b", "c.go"), "package c")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "nested file")
	head := sha(t, dir, "HEAD")

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/b/c.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestChangedMultipleFiles verifies multiple files are returned.
func TestChangedMultipleFiles(t *testing.T) {
	dir := initRepo(t)
	base := sha(t, dir, "HEAD")

	writeFile(t, filepath.Join(dir, "a.go"), "package a")
	writeFile(t, filepath.Join(dir, "b.go"), "package b")
	writeFile(t, filepath.Join(dir, "sub", "c.go"), "package sub")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "add several")
	head := sha(t, dir, "HEAD")

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"a.go", "b.go", "sub/c.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestChangedNoChanges verifies nil is returned when SHAs are identical.
func TestChangedNoChanges(t *testing.T) {
	dir := initRepo(t)
	h := sha(t, dir, "HEAD")
	got, err := Changed(dir, h, h)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestChangedRename verifies that a renamed file reports both old and new
// paths (--no-renames disables rename detection so both appear).
func TestChangedRename(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "old.go"), "package main")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "add old")
	base := sha(t, dir, "HEAD")

	run(t, dir, "git", "mv", "old.go", "new.go")
	run(t, dir, "git", "commit", "-m", "rename")
	head := sha(t, dir, "HEAD")

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"new.go", "old.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (both old and new paths)", got, want)
	}
}

// TestChangedBadRef verifies that an invalid ref returns an error.
func TestChangedBadRef(t *testing.T) {
	dir := initRepo(t)
	_, err := Changed(dir, "nonexistent", "HEAD")
	if err == nil {
		t.Fatal("expected error for bad ref")
	}
}
