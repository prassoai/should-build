package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/prassoai/should-build/depgraph"
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

// TestRunExplicitSelection exercises selection: explicit end-to-end over two
// real commit ranges, reproducing the case the mode was built for: a VM image
// bake that must run when its Packer recipe changes and at no other time.
//
// The first range is the one that used to waste ~90 machine-minutes a time —
// a go.mod bump plus a file no target claims. Both are repo-wide signals that
// name none of the image's inputs, so the explicit target must skip while the
// ordinary targets still fail open on them. The second range touches the
// recipe itself, which must select it.
func TestRunExplicitSelection(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")

	writeFile(t, filepath.Join(dir, "should-build.yaml"), `
global:
  trigger_all:
    - "go.mod"
unknown_file: trigger_all
targets:
  api:
    lang: none
    include:
      - "cmd/api/**"
  base-images:
    lang: none
    selection: explicit
    include:
      - "terraform/packer/**"
    exclude:
      - "terraform/packer/tests/**"
`)
	writeFile(t, filepath.Join(dir, "terraform", "packer", "gce.pkr.hcl"), "build {}")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")
	base := gitSHA(t, dir, "HEAD")

	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")
	writeFile(t, filepath.Join(dir, "native", "App.swift"), "// orphan")
	writeFile(t, filepath.Join(dir, "terraform", "packer", "tests", "smoke.sh"), "true")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "repo-wide churn")
	churn := gitSHA(t, dir, "HEAD")

	writeFile(t, filepath.Join(dir, "terraform", "packer", "gce.pkr.hcl"), "build { provisioner {} }")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "change the recipe")
	recipe := gitSHA(t, dir, "HEAD")

	build := func(t *testing.T, from, to string) map[string]bool {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run([]string{"--repo", dir, "--json", "--verbose", from, to}, &stdout, &stderr); code != 0 {
			t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
		}
		var results []eval.Result
		if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
			t.Fatalf("decoding output %q: %v", stdout.String(), err)
		}
		got := make(map[string]bool, len(results))
		for _, r := range results {
			got[r.Target] = r.Build
		}
		return got
	}

	churnBuilds := build(t, base, churn)
	if !churnBuilds["api"] {
		t.Error("api should build — go.mod matches trigger_all and native/ is an orphan")
	}
	if churnBuilds["base-images"] {
		t.Error("base-images must not build: go.mod, an orphan file and an excluded recipe test name none of its inputs")
	}

	recipeBuilds := build(t, churn, recipe)
	if !recipeBuilds["base-images"] {
		t.Error("base-images should build — its recipe changed")
	}
	if recipeBuilds["api"] {
		t.Error("api must not build: the recipe file is claimed by base-images, so it is not an orphan")
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

// TestRunUnclassifiedFailsWithoutOutput prevents consumers from publishing a
// selection when strict classification fails, in every CLI output mode.
func TestRunUnclassifiedFailsWithoutOutput(t *testing.T) {
	dir, base, _ := setupTestRepo(t)
	writeFile(t, filepath.Join(dir, "should-build.yaml"), "unknown_file: error\ntargets:\n  api:\n    lang: none\n    include: [cmd/api/**, should-build.yaml, docs/**]\n")
	writeFile(t, filepath.Join(dir, "unowned.txt"), "unknown input")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "unclassified input")
	for _, mode := range []string{"--json", "--quiet", "--verbose"} {
		t.Run(mode, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"--repo", dir, mode, base, "HEAD"}, &stdout, &stderr); code != 2 {
				t.Fatalf("exit %d, want 2: %s", code, &stderr)
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "unclassified inputs: unowned.txt") {
				t.Fatalf("stdout=%q stderr=%q", &stdout, &stderr)
			}
		})
	}
}

// TestRunDeletedGoFileSelectsFormerConsumer requires deleting a Go file to
// select exactly the targets that imported it. The deleted file is absent from
// the head checkout, so only the base revision's graph can attribute it;
// without that it is unclassified and unknown_file: error rejects the change.
func TestRunDeletedGoFileSelectsFormerConsumer(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, "should-build.yaml"), `
unknown_file: error
targets:
  api:
    path: ./cmd/api
  worker:
    path: ./cmd/worker
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(dir, "cmd/api/main.go"), "package main\nimport _ \"example.com/test/lib\"\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "cmd/worker/main.go"), "package main\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "lib/foo.go"), "package lib\n")
	writeFile(t, filepath.Join(dir, "lib/bar.go"), "package lib\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	base := gitSHA(t, dir, "HEAD")
	gitRun(t, dir, "rm", "-q", "lib/foo.go")
	gitRun(t, dir, "commit", "-m", "head")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--json", "--repo", dir, base, gitSHA(t, dir, "HEAD")}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var results []eval.Result
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	want := []eval.Result{
		{Target: "api", Build: true, Files: []eval.FileMatch{{Path: "lib/foo.go", Reason: "go-dep"}}},
		{Target: "worker", Files: []eval.FileMatch{}},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

// TestRunGOOSSelectsPlatformFiles requires a target declaring goos to be
// selected by a file compiled only for one of those systems, whether the
// change edits or deletes it, while a target built only for the analyzer's
// own system is not. Without per-platform graphs, foo_darwin.go is in no
// graph on Linux and unknown_file: error rejects the change.
func TestRunGOOSSelectsPlatformFiles(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, "should-build.yaml"), `
unknown_file: error
targets:
  cli:
    path: ./cmd/cli
    goos: [linux, darwin]
  server:
    path: ./cmd/server
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(dir, "cmd/cli/main.go"), "package main\nimport _ \"example.com/test/lib\"\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "cmd/server/main.go"), "package main\nimport _ \"example.com/test/lib\"\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "lib/lib.go"), "package lib\n")
	writeFile(t, filepath.Join(dir, "lib/prompt_darwin.go"), "package lib\n")
	writeFile(t, filepath.Join(dir, "lib/prompt_windows.go"), "package lib\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	base := gitSHA(t, dir, "HEAD")
	writeFile(t, filepath.Join(dir, "lib/prompt_darwin.go"), "package lib\nconst Changed = true\n")
	gitRun(t, dir, "commit", "-qam", "edit")
	edited := gitSHA(t, dir, "HEAD")
	gitRun(t, dir, "rm", "-q", "lib/prompt_darwin.go")
	gitRun(t, dir, "commit", "-qm", "delete")

	for _, head := range []string{edited, gitSHA(t, dir, "HEAD")} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"--json", "--repo", dir, base, head}, &stdout, &stderr); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr.String())
		}
		var results []eval.Result
		if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
			t.Fatal(err)
		}
		want := []eval.Result{
			{Target: "cli", Build: true, Files: []eval.FileMatch{{Path: "lib/prompt_darwin.go", Reason: "go-dep"}}},
			{Target: "server", Files: []eval.FileMatch{}},
		}
		if !reflect.DeepEqual(results, want) {
			t.Fatalf("%s: got %+v, want %+v", head, results, want)
		}
	}
}

// TestRunFileLeavingPlatformSelectsFormerConsumer requires a target to be
// selected when an edit drops an existing file from its platform's build. The
// file still compiles for darwin, so the decision to load the base must be
// made per GOOS: one made for all platforms at once could skip the linux base
// and leave server unselected though its binary lost the file.
func TestRunFileLeavingPlatformSelectsFormerConsumer(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, "should-build.yaml"), `
unknown_file: error
targets:
  cli:
    path: ./cmd/cli
    goos: [linux, darwin]
  server:
    path: ./cmd/server
    goos: [linux]
`)
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(dir, "cmd/cli/main.go"), "package main\nimport _ \"example.com/test/lib\"\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "cmd/server/main.go"), "package main\nimport _ \"example.com/test/lib\"\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "lib/lib.go"), "package lib\n")
	writeFile(t, filepath.Join(dir, "lib/prompt.go"), "package lib\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	base := gitSHA(t, dir, "HEAD")
	writeFile(t, filepath.Join(dir, "lib/prompt.go"), "//go:build darwin\n\npackage lib\n")
	gitRun(t, dir, "commit", "-qam", "darwin only")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--json", "--repo", dir, base, gitSHA(t, dir, "HEAD")}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var results []eval.Result
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	want := []eval.Result{
		{Target: "cli", Build: true, Files: []eval.FileMatch{{Path: "lib/prompt.go", Reason: "go-dep"}}},
		{Target: "server", Build: true, Files: []eval.FileMatch{{Path: "lib/prompt.go", Reason: "go-dep"}}},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("got %+v, want %+v", results, want)
	}
}

// TestNeedsBase requires the base graph to load exactly when a changed file
// compiled at the base and no longer does: it was deleted, a directory
// replaced it, or its name, build constraint or cgo import now excludes it.
// Build tags from GOFLAGS count, as they do for the graphs. A deleted non-Go
// file may have been embedded, even from testdata, so it loads the base too.
// Edits that keep a file compiling, files that never compiled (an
// integration-tagged helper without the tag), test files and testdata change
// in nearly every commit and must skip the base checkout.
func TestNeedsBase(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	files := map[string]string{
		"lib/lib.go":              "package lib\n",
		"lib/lib_test.go":         "package lib\n",
		"lib/gone.go":             "package lib\n",
		"lib/gone_test.go":        "package lib\n",
		"lib/integration.go":      "//go:build integration\n\npackage lib\n",
		"lib/integration_gone.go": "//go:build integration\n\npackage lib\n",
		"lib/moved.go":            "package lib\n",
		"lib/cgo.go":              "package lib\n",
		"lib/broken.go":           "package lib\n",
		"lib/testdata/bad.go":     "package\n",
		"static/logo.svg":         "",
		"lib/testdata/golden.txt": "",
		"lib/replaced.go":         "package lib\n",
		"web/app.tsx":             "",
	}
	for name, content := range files {
		writeFile(t, filepath.Join(dir, name), content)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	base := gitSHA(t, dir, "HEAD")
	gitRun(t, dir, "rm", "-q", "lib/replaced.go")
	for name, content := range map[string]string{
		"lib/lib.go":               "package lib\nconst A = 1\n",
		"lib/lib_test.go":          "package lib\nconst B = 1\n",
		"lib/integration.go":       "//go:build integration\n\npackage lib\nconst C = 1\n",
		"lib/added_integration.go": "//go:build integration\n\npackage lib\n",
		"lib/moved.go":             "//go:build darwin\n\npackage lib\n",
		"lib/cgo.go":               "package lib\n\nimport \"C\"\n",
		"lib/broken.go":            "package\n",
		"lib/testdata/bad.go":      "package\n\n",
		"lib/replaced.go/doc.go":   "package replaced\n",
		"web/app.tsx":              "x",
	} {
		writeFile(t, filepath.Join(dir, name), content)
	}
	for _, name := range []string{"lib/gone.go", "lib/gone_test.go", "lib/integration_gone.go", "static/logo.svg", "lib/testdata/golden.txt"} {
		gitRun(t, dir, "rm", "-q", name)
	}

	linux, darwin := []string{"GOOS=linux"}, []string{"GOOS=darwin"}
	integration := []string{"GOOS=linux", "GOFLAGS=-tags=integration"}
	for _, tc := range []struct {
		env     []string
		changed []string
		want    bool
	}{
		{linux, nil, false},
		{linux, []string{"lib/lib.go", "lib/lib_test.go", "web/app.tsx", "lib/testdata/bad.go"}, false},
		{linux, []string{"lib/integration.go", "lib/added_integration.go", "lib/integration_gone.go"}, false},
		{linux, []string{"lib/gone_test.go"}, false},
		{linux, []string{"lib/gone.go"}, true},
		{linux, []string{"static/logo.svg"}, true},
		{linux, []string{"lib/testdata/golden.txt"}, true},
		{linux, []string{"lib/moved.go"}, true},
		{darwin, []string{"lib/moved.go"}, false},
		{linux, []string{"lib/cgo.go"}, true},
		{linux, []string{"lib/broken.go"}, true},
		{linux, []string{"lib/replaced.go"}, true},
		{integration, []string{"lib/integration_gone.go"}, true},
		{integration, []string{"lib/integration.go", "lib/added_integration.go"}, false},
	} {
		ctx, err := (depgraph.Go{Env: tc.env}).Context(dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := needsBase(dir, base, ctx, tc.changed)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("needsBase(%v, %v) = %v, want %v", tc.env, tc.changed, got, tc.want)
		}
	}
}
