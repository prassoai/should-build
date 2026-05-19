package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// git runs a git command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// writeFile creates a file with the given content, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sha returns HEAD's full SHA in dir.
func sha(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return string(out[:len(out)-1]) // trim newline
}

// initRepo creates a git repo with an initial commit containing a.go and b.go.
func initRepo(t *testing.T) (dir, baseSHA string) {
	t.Helper()
	dir = t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@test.com")
	git(t, dir, "config", "user.name", "Test")

	writeFile(t, filepath.Join(dir, "a.go"), "package a")
	writeFile(t, filepath.Join(dir, "sub/b.go"), "package sub")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")
	return dir, sha(t, dir)
}

// TestChangedAddAndModify verifies that added and modified files both
// appear in the diff output.
func TestChangedAddAndModify(t *testing.T) {
	dir, base := initRepo(t)

	writeFile(t, filepath.Join(dir, "a.go"), "package a\n// modified")
	writeFile(t, filepath.Join(dir, "c.go"), "package c")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "changes")
	head := sha(t, dir)

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"a.go", "c.go"}
	if !slices.Equal(got, want) {
		t.Errorf("Changed = %v, want %v", got, want)
	}
}

// TestChangedDelete verifies that deleted files appear in the diff.
func TestChangedDelete(t *testing.T) {
	dir, base := initRepo(t)

	os.Remove(filepath.Join(dir, "a.go"))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "delete a.go")
	head := sha(t, dir)

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "a.go") {
		t.Errorf("Changed = %v, want a.go in list", got)
	}
}

// TestChangedSubdirectory verifies that files in subdirectories are
// reported with their full relative path.
func TestChangedSubdirectory(t *testing.T) {
	dir, base := initRepo(t)

	writeFile(t, filepath.Join(dir, "sub/b.go"), "package sub\n// modified")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "modify sub/b.go")
	head := sha(t, dir)

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"sub/b.go"}) {
		t.Errorf("Changed = %v, want [sub/b.go]", got)
	}
}

// TestChangedNoChanges verifies that identical SHAs produce an empty list.
func TestChangedNoChanges(t *testing.T) {
	dir, base := initRepo(t)

	got, err := Changed(dir, base, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Changed = %v, want empty", got)
	}
}

// TestChangedInvalidRef verifies that an invalid ref produces an error.
func TestChangedInvalidRef(t *testing.T) {
	dir, _ := initRepo(t)

	_, err := Changed(dir, "deadbeef", "HEAD")
	if err == nil {
		t.Error("Changed with invalid ref should fail")
	}
}

// TestChangedRename verifies that renamed files appear in the diff output.
// Git may report renames differently depending on version and config, but
// at minimum the new name must appear.
func TestChangedRename(t *testing.T) {
	dir, base := initRepo(t)

	os.Rename(filepath.Join(dir, "a.go"), filepath.Join(dir, "renamed.go"))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "rename a.go -> renamed.go")
	head := sha(t, dir)

	got, err := Changed(dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	hasRenamed := false
	for _, f := range got {
		if f == "renamed.go" {
			hasRenamed = true
		}
	}
	if !hasRenamed {
		t.Errorf("Changed = %v, want renamed.go in list", got)
	}
}
