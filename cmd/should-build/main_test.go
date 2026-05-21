package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prassoai/should-build/eval"
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

// TestRunVersion verifies that --version prints version info and exits 0.
// The version/commit variables are set by ldflags during release builds;
// in tests they retain their default values ("dev" / "unknown").
func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "should-build") {
		t.Errorf("expected program name in version output: %s", out)
	}
	if !strings.Contains(out, "dev") {
		t.Errorf("expected default version 'dev' in output: %s", out)
	}
	if !strings.Contains(out, "unknown") {
		t.Errorf("expected default commit 'unknown' in output: %s", out)
	}
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
// Without --explain, no explanation is written to stderr.
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
	// Without --explain, no explanation should appear on stderr.
	if strings.Contains(stderr.String(), "should-build:") {
		t.Errorf("--json without --explain should not write explanation to stderr, got:\n%s", stderr.String())
	}
}

// TestRunJSONVerbose verifies that --json --verbose preserves the Rule field.
// --verbose controls JSON shape only; --explain is a separate flag.
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
	// --verbose alone does not write explanation to stderr.
	if strings.Contains(stderr.String(), "should-build:") {
		t.Errorf("--verbose without --explain should not write explanation to stderr, got:\n%s", stderr.String())
	}
}

// TestRunExplain verifies that --explain writes a human-readable explanation
// to stderr, independent of --verbose. This is what the GitHub Action uses
// to make CI logs diagnosable at a glance.
func TestRunExplain(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--json", "--explain", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}

	// JSON on stdout should still omit rules (--verbose not passed).
	if strings.Contains(stdout.String(), `"rule"`) {
		t.Errorf("--explain without --verbose should omit rule field in JSON:\n%s", stdout.String())
	}

	// Explanation on stderr.
	explain := stderr.String()
	if !strings.Contains(explain, "should-build: 2 targets evaluated, 1 rebuilding") {
		t.Errorf("explanation should show summary header, got:\n%s", explain)
	}
	if !strings.Contains(explain, "api: rebuild") {
		t.Errorf("explanation should show api rebuilding, got:\n%s", explain)
	}
	if !strings.Contains(explain, "web: skip") {
		t.Errorf("explanation should show web skipping, got:\n%s", explain)
	}
	if !strings.Contains(explain, "handler.go") {
		t.Errorf("explanation should list triggering files, got:\n%s", explain)
	}
	if !strings.Contains(explain, "cmd/api/**") {
		t.Errorf("explanation should show matching rule, got:\n%s", explain)
	}
}

// TestRunExplainVerbose verifies that --explain and --verbose compose:
// explanation on stderr, rules in JSON on stdout.
func TestRunExplainVerbose(t *testing.T) {
	dir, base, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--json", "--verbose", "--explain", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"rule"`) {
		t.Errorf("--verbose should include rule field in JSON:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "api: rebuild") {
		t.Errorf("--explain should write explanation to stderr:\n%s", stderr.String())
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

// TestFormatExplainResult directly tests the pure formatting helper.
func TestFormatExplainResult(t *testing.T) {
	tests := []struct {
		name   string
		result eval.Result
		want   string
	}{
		{
			name:   "skip",
			result: eval.Result{Target: "web", Files: []eval.FileMatch{}},
			want:   "  web: skip",
		},
		{
			name: "rebuild with rule",
			result: eval.Result{Target: "api", Build: true, Files: []eval.FileMatch{
				{Path: "cmd/api/handler.go", Reason: "include", Rule: "cmd/api/**"},
			}},
			want: "  api: rebuild (1 file)\n    cmd/api/handler.go  (include: cmd/api/**)",
		},
		{
			name: "rebuild without rule",
			result: eval.Result{Target: "api", Build: true, Files: []eval.FileMatch{
				{Path: "pkg/auth/token.go", Reason: "go-dep"},
			}},
			want: "  api: rebuild (1 file)\n    pkg/auth/token.go  (go-dep)",
		},
		{
			name: "rebuild multiple files",
			result: eval.Result{Target: "api", Build: true, Files: []eval.FileMatch{
				{Path: "cmd/api/handler.go", Reason: "include", Rule: "cmd/api/**"},
				{Path: "go.mod", Reason: "trigger-all", Rule: "go.mod"},
			}},
			want: "  api: rebuild (2 files)\n    cmd/api/handler.go  (include: cmd/api/**)\n    go.mod  (trigger-all: go.mod)",
		},
		{
			name: "rebuild from trigger",
			result: eval.Result{Target: "vm", Build: true, Files: []eval.FileMatch{
				{Reason: "triggered-by", Rule: "control"},
			}},
			want: "  vm: rebuild (1 trigger)\n    triggered by control",
		},
		{
			name: "rebuild from files and trigger",
			result: eval.Result{Target: "vm", Build: true, Files: []eval.FileMatch{
				{Path: "cmd/vm/main.go", Reason: "include", Rule: "cmd/vm/**"},
				{Reason: "triggered-by", Rule: "control"},
			}},
			want: "  vm: rebuild (1 file, 1 trigger)\n    cmd/vm/main.go  (include: cmd/vm/**)\n    triggered by control",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatExplainResult(tt.result)
			if got != tt.want {
				t.Errorf("formatExplainResult() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

// TestRunTargetTriggerExpansion verifies that --target evaluates the named
// target initially, but trigger propagation pulls in additional targets.
// "should-build --target control" with control.triggers=[vm] should output
// both control (build=true from include) and vm (build=true from triggered-by).
func TestRunTargetTriggerExpansion(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")

	writeFile(t, filepath.Join(dir, "should-build.yaml"), `
targets:
  control:
    lang: none
    include:
      - "cmd/control/**"
    triggers:
      - vm
  vm:
    lang: none
    include:
      - "cmd/vm/**"
  other:
    lang: none
    include:
      - "other/**"
unknown_file: ignore
`)
	writeFile(t, filepath.Join(dir, ".gitkeep"), "")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")
	base := gitSHA(t, dir, "HEAD")

	writeFile(t, filepath.Join(dir, "cmd", "control", "main.go"), "package main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add control code")
	head := gitSHA(t, dir, "HEAD")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "--json", "--verbose", "--target", "control", base, head}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	// control should build from include match.
	if !strings.Contains(out, `"target": "control"`) {
		t.Errorf("output should contain control target:\n%s", out)
	}
	// vm should build from trigger expansion (not in --target, but triggered by control).
	if !strings.Contains(out, `"target": "vm"`) {
		t.Errorf("output should contain vm target (trigger expansion):\n%s", out)
	}
	if !strings.Contains(out, `"triggered-by"`) {
		t.Errorf("output should contain triggered-by reason:\n%s", out)
	}
	// "other" should NOT appear — not in --target and not triggered.
	if strings.Contains(out, `"other"`) {
		t.Errorf("output should not contain other target:\n%s", out)
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

// TestRunInvalidRef verifies that the CLI exits non-zero when git receives an
// unresolvable ref. This is the primary exit-code safety test: callers must
// never see exit 0 when git fails, because exit 0 means "I evaluated
// successfully and here are the results." An invalid base SHA with exit 0
// would silently produce no targets, causing deploy-everything to not trigger.
func TestRunInvalidRef(t *testing.T) {
	dir, _, head := setupTestRepo(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--repo", dir, "nonexistent-sha", head}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit for invalid ref, got 0; stderr: %s", stderr.String())
	}
	if code != 2 {
		t.Errorf("expected exit 2 for git failure, got %d", code)
	}
	if !strings.Contains(stderr.String(), "git diff") {
		t.Errorf("stderr should mention git diff, got: %s", stderr.String())
	}
}

// errWriter is an io.Writer that always returns an error, used to simulate
// broken stdout (pipe closed, disk full, etc.).
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// TestRunWriteError verifies that the CLI exits non-zero when stdout fails.
// A broken pipe must not be silently swallowed — exit 0 with partial output
// is worse than exit 2 with an error on stderr.
func TestRunWriteError(t *testing.T) {
	dir, base, head := setupTestRepo(t)
	brokenStdout := errWriter{errors.New("broken pipe")}

	t.Run("table", func(t *testing.T) {
		var stderr bytes.Buffer
		code := run([]string{"--repo", dir, base, head}, brokenStdout, &stderr)
		if code == 0 {
			t.Fatalf("expected non-zero exit when stdout fails, got 0")
		}
	})

	t.Run("json", func(t *testing.T) {
		var stderr bytes.Buffer
		code := run([]string{"--repo", dir, "--json", base, head}, brokenStdout, &stderr)
		if code == 0 {
			t.Fatalf("expected non-zero exit when stdout fails, got 0")
		}
	})
}
