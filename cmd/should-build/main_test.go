package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitSHA(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// setupTestRepo creates a git repo with a should-build.yaml and two commits.
// Returns (dir, baseSHA, headSHA).
func setupTestRepo(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")

	writeFile(t, filepath.Join(dir, "should-build.yaml"), `
global:
  ignore:
    - "docs/**"
  trigger_all:
    - "go.mod"
unknown_file: ignore
targets:
  api:
    lang: none
    include:
      - "cmd/api/**"
  web:
    lang: none
    include:
      - "web/**"
`)
	writeFile(t, filepath.Join(dir, "cmd", "api", "main.go"), "package main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")
	base := gitSHA(t, dir, "HEAD")

	writeFile(t, filepath.Join(dir, "cmd", "api", "handler.go"), "package main")
	writeFile(t, filepath.Join(dir, "docs", "readme.md"), "# docs")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "changes")
	head := gitSHA(t, dir, "HEAD")

	return dir, base, head
}

// TestRunTable tests the full CLI run function with table output.
func TestRunTable(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "api") || !strings.Contains(out, "yes") {
		t.Errorf("expected api=yes in output:\n%s", out)
	}
	if !strings.Contains(out, "web") || !strings.Contains(out, "no") {
		t.Errorf("expected web=no in output:\n%s", out)
	}
}

// TestRunJSON tests JSON output format.
func TestRunJSON(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--json", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"build": true`) {
		t.Errorf("expected JSON with build:true:\n%s", out)
	}
	// Non-verbose JSON should not contain the "rule" field.
	if strings.Contains(out, `"rule"`) {
		t.Errorf("non-verbose JSON should omit rule field:\n%s", out)
	}
}

// TestRunJSONVerbose verifies that --json --verbose preserves the Rule field.
func TestRunJSONVerbose(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--json", "--verbose", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"rule"`) {
		t.Errorf("verbose JSON should include rule field:\n%s", out)
	}
	if !strings.Contains(out, `cmd/api/**`) {
		t.Errorf("verbose JSON should show the matching glob pattern:\n%s", out)
	}
}

// TestRunTableVerbose verifies that --verbose in table mode emits one row
// per (target, file) with the matching rule. This is the same behavior as
// --json --verbose but in human-readable form.
func TestRunTableVerbose(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--verbose", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	// Verbose table must include the glob rule pattern.
	if !strings.Contains(out, "cmd/api/**") {
		t.Errorf("verbose table should show glob rule:\n%s", out)
	}
	if !strings.Contains(out, "include:") {
		t.Errorf("verbose table should show reason with colon:\n%s", out)
	}
	// handler.go triggers api via the include pattern — verify the file appears.
	if !strings.Contains(out, "handler.go") {
		t.Errorf("verbose table should list triggering file:\n%s", out)
	}
}

// TestRunQuietBuild tests quiet mode when a target needs rebuilding.
func TestRunQuietBuild(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--quiet", base, head}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit 1 (rebuild needed), got %d", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("quiet mode should produce no stdout, got: %s", stdout.String())
	}
}

// TestRunQuietNoBuild tests quiet mode when nothing needs rebuilding.
func TestRunQuietNoBuild(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	// Only web target; docs/readme.md is ignored, so nothing triggers web.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--quiet", "--target", "web", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit 0 (nothing to rebuild), got %d", code)
	}
}

// TestRunQuietJSON verifies that --quiet --json is rejected.
func TestRunQuietJSON(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--quiet", "--json", base, head}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit 2 for --quiet --json, got %d", code)
	}
	if !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Errorf("expected mutually exclusive error, got: %s", stderr.String())
	}
}

// TestRunTargetFilter tests --target flag filters to specific targets.
func TestRunTargetFilter(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--json", "--target", "web", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), `"api"`) {
		t.Error("--target web should exclude api from output")
	}
}

// TestRunUnknownTarget tests that an unknown --target flag returns exit 2.
func TestRunUnknownTarget(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--target", "nonexistent", base, head}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit 2 for unknown target, got %d", code)
	}
}

// TestRunMissingArgs tests that missing positional args returns exit 2.
func TestRunMissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// TestRunMissingConfig tests that a missing config file returns exit 2.
func TestRunMissingConfig(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "abc", "def"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "reading config") {
		t.Errorf("expected config error, got: %s", stderr.String())
	}
}

// TestRunTargetTemplate verifies that {target} template expansion works
// end-to-end through the CLI.
func TestRunTargetTemplate(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")

	writeFile(t, filepath.Join(dir, "should-build.yaml"), `
targets:
  myservice:
    lang: none
    include:
      - "targets/{target}/conf/{target}-*.hjson"
unknown_file: ignore
`)
	writeFile(t, filepath.Join(dir, ".gitkeep"), "")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")
	base := gitSHA(t, dir, "HEAD")

	writeFile(t, filepath.Join(dir, "targets", "myservice", "conf", "myservice-prod.hjson"), "{}")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add config")
	head := gitSHA(t, dir, "HEAD")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--json", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"build": true`) {
		t.Errorf("expected {target} expansion to trigger build:\n%s", out)
	}
	if !strings.Contains(out, "myservice-prod.hjson") {
		t.Errorf("expected hjson file in output:\n%s", out)
	}
}
