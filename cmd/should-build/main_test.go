package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func testWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// setupFixtureRepo creates a git repo with a Go module, should-build.yaml,
// web/ and docs/ directories. Returns (dir, baseSHA, headSHA) where head
// includes changes to cmd/api/main.go, web/index.html, and docs/readme.md.
func setupFixtureRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()

	testGit(t, dir, "init")
	testGit(t, dir, "config", "user.email", "test@test.com")
	testGit(t, dir, "config", "user.name", "Test")

	testWrite(t, filepath.Join(dir, "go.mod"), "module example.com/repo\n\ngo 1.23\n")
	testWrite(t, filepath.Join(dir, "cmd/api/main.go"), "package main\n\nfunc main() {}\n")
	testWrite(t, filepath.Join(dir, "web/index.html"), "<html></html>")
	testWrite(t, filepath.Join(dir, "docs/readme.md"), "# Docs")
	testWrite(t, filepath.Join(dir, "should-build.yaml"), `global:
  ignore:
    - "docs/**"
    - "**/*.md"
  trigger_all:
    - "go.mod"
unknown_file: ignore
targets:
  api:
    path: ./cmd/api
  web:
    lang: none
    include:
      - "web/**"
`)

	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "initial")
	base = testSHA(t, dir)

	testWrite(t, filepath.Join(dir, "cmd/api/main.go"), "package main\n\n// modified\nfunc main() {}\n")
	testWrite(t, filepath.Join(dir, "web/index.html"), "<html>updated</html>")
	testWrite(t, filepath.Join(dir, "docs/readme.md"), "# Updated Docs")
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "changes")
	head = testSHA(t, dir)

	return dir, base, head
}

// TestEndToEnd exercises the full pipeline: config loading, git diff,
// Go dep-graph analysis, evaluation, and all output modes.
func TestEndToEnd(t *testing.T) {
	dir, base, head := setupFixtureRepo(t)
	cfgPath := filepath.Join(dir, "should-build.yaml")

	t.Run("rebuild_decisions", func(t *testing.T) {
		results, err := evaluate(cfgPath, dir, base, head, nil)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("got %d results, want 2", len(results))
		}

		// Results are sorted alphabetically: api, web.
		if results[0].Target != "api" || !results[0].Build {
			t.Errorf("api: target=%q build=%v, want api/true", results[0].Target, results[0].Build)
		}
		if results[1].Target != "web" || !results[1].Build {
			t.Errorf("web: target=%q build=%v, want web/true", results[1].Target, results[1].Build)
		}

		// docs/readme.md must be globally ignored.
		for _, r := range results {
			for _, f := range r.Files {
				if strings.HasPrefix(f.Path, "docs/") {
					t.Errorf("docs/ file %q should be globally ignored, but triggered %s", f.Path, r.Target)
				}
			}
		}
	})

	t.Run("no_changes", func(t *testing.T) {
		results, err := evaluate(cfgPath, dir, head, head, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Build {
				t.Errorf("%s should not build when no files changed", r.Target)
			}
		}
	})

	t.Run("target_filter", func(t *testing.T) {
		results, err := evaluate(cfgPath, dir, base, head, []string{"web"})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].Target != "web" {
			t.Errorf("--target web: got %v, want [web]", results)
		}
	})

	t.Run("unknown_target_error", func(t *testing.T) {
		_, err := evaluate(cfgPath, dir, base, head, []string{"web", "nope", "also_nope"})
		if err == nil {
			t.Fatal("expected error for unknown targets")
		}
		if !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "also_nope") {
			t.Errorf("error should list all unknown targets: %v", err)
		}
	})

	t.Run("json_output", func(t *testing.T) {
		results, err := evaluate(cfgPath, dir, base, head, nil)
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		if err := printJSON(&buf, results, true); err != nil {
			t.Fatal(err)
		}

		var parsed []struct {
			Target string `json:"target"`
			Build  bool   `json:"build"`
			Files  []struct {
				Path   string `json:"path"`
				Reason string `json:"reason"`
				Rule   string `json:"rule"`
			} `json:"files"`
		}
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("JSON unmarshal: %v\n%s", err, buf.String())
		}
		if len(parsed) != 2 {
			t.Fatalf("JSON: got %d results, want 2", len(parsed))
		}
		// With verbose, Rule is populated for files that triggered.
		for _, r := range parsed {
			for _, f := range r.Files {
				if f.Rule == "" {
					t.Errorf("JSON verbose: file %q in %q has empty rule", f.Path, r.Target)
				}
			}
		}
	})

	t.Run("json_non_verbose_strips_rule", func(t *testing.T) {
		results, err := evaluate(cfgPath, dir, base, head, nil)
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		if err := printJSON(&buf, results, false); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), `"rule"`) {
			t.Error("non-verbose JSON should not contain rule field")
		}
	})

	t.Run("table_output", func(t *testing.T) {
		results, err := evaluate(cfgPath, dir, base, head, nil)
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		if err := printTable(&buf, results); err != nil {
			t.Fatal(err)
		}
		table := buf.String()
		if !strings.Contains(table, "TARGET") || !strings.Contains(table, "BUILD") {
			t.Errorf("table missing header:\n%s", table)
		}
		if !strings.Contains(table, "api") || !strings.Contains(table, "yes") {
			t.Errorf("table missing api/yes:\n%s", table)
		}
	})

	t.Run("quiet_rebuild_exit_code", func(t *testing.T) {
		results, err := evaluate(cfgPath, dir, base, head, nil)
		if err != nil {
			t.Fatal(err)
		}
		anyBuild := false
		for _, r := range results {
			if r.Build {
				anyBuild = true
				break
			}
		}
		if !anyBuild {
			t.Error("rebuild case: expected at least one target to build")
		}
	})

	t.Run("quiet_no_rebuild_exit_code", func(t *testing.T) {
		results, err := evaluate(cfgPath, dir, head, head, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Build {
				t.Errorf("no-rebuild case: %s should not build", r.Target)
			}
		}
	})
}

// TestConfigDefaultPath verifies that when --config is not set, evaluate
// looks for should-build.yaml inside the repo directory (not cwd).
func TestConfigDefaultPath(t *testing.T) {
	dir, base, head := setupFixtureRepo(t)

	// Call evaluate with the config at <repo>/should-build.yaml.
	cfgPath := filepath.Join(dir, "should-build.yaml")
	results, err := evaluate(cfgPath, dir, base, head, nil)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
}
