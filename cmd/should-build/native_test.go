package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/prassoai/should-build/eval"
)

// TestRunNativeInputs prevents an explicit target from silently skipping a
// native change that changes its executable, and keeps other targets scoped.
func TestRunNativeInputs(t *testing.T) {
	t.Setenv("GOFLAGS", "")
	probe := t.TempDir()
	writeFile(t, filepath.Join(probe, "go.mod"), "module example.com/probe\n\ngo 1.23\n")
	writeFile(t, filepath.Join(probe, "main.go"), "package main\nimport \"C\"\nfunc main() {}\n")
	cmd := exec.Command("go", "build", "-o", filepath.Join(probe, "probe"), ".")
	cmd.Dir = probe
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cgo build unavailable: %v\n%s", err, out)
	}

	root := t.TempDir()
	gitRun(t, root, "init")
	gitRun(t, root, "config", "user.email", "test@test.com")
	gitRun(t, root, "config", "user.name", "Test")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.23\n")
	writeFile(t, filepath.Join(root, "should-build.yaml"), `
global:
  ignore: ["internal/native/ignored.h"]
  trigger_all: ["go.mod"]
targets:
  one:
    path: ./cmd/one
    selection: explicit
    triggers: [package]
  two:
    path: ./cmd/two
  other:
    path: ./cmd/other
  excluded:
    path: ./cmd/one
    selection: explicit
    exclude: ["internal/native/**"]
  package:
    lang: none
    selection: explicit
`)
	for _, name := range []string{"one", "two"} {
		writeFile(t, filepath.Join(root, "cmd", name, "main.go"), "package main\nimport (\"fmt\"; \"example.com/test/internal/bridge\")\nfunc main() { fmt.Println(bridge.Value()) }\n")
	}
	writeFile(t, filepath.Join(root, "cmd/other/main.go"), "package main\nfunc main() {}\n")
	writeFile(t, filepath.Join(root, "internal/bridge/bridge.go"), "package bridge\nimport \"example.com/test/internal/native\"\nfunc Value() int { return native.Value() }\n")
	writeFile(t, filepath.Join(root, "internal/native/ignored.h"), "// ignored by configuration\n")
	initial := map[string]string{
		"internal/native/value.go": "package native\n/*\n#include \"value.h\"\n*/\nimport \"C\"\nfunc Value() int { return int(C.value()) + 1 }\n",
		"internal/native/value.c":  "#include \"value.h\"\nint value(void) { return 40 + OFFSET; }\n",
		"internal/native/value.h":  "#define OFFSET 1\nint value(void);\n",
	}
	for name, contents := range initial {
		writeFile(t, filepath.Join(root, name), contents)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "initial")

	for _, tc := range []struct {
		path, content string
		selected      map[string]eval.FileMatch
	}{
		{path: "internal/native/value.go", content: "package native\n/*\n#include \"value.h\"\n*/\nimport \"C\"\nfunc Value() int { return int(C.value()) + 2 }\n"},
		{path: "internal/native/value.c", content: "#include \"value.h\"\nint value(void) { return 41 + OFFSET; }\n"},
		{path: "internal/native/value.h", content: "#define OFFSET 2\nint value(void);\n"},
		{path: "internal/native/ignored.h", content: "// changed but globally ignored\n", selected: map[string]eval.FileMatch{}},
		{path: "cmd/other/main.go", content: "package main\nfunc main() { println(1) }\n", selected: map[string]eval.FileMatch{
			"other": {Path: "cmd/other/main.go", Reason: "go-dep"},
		}},
		{path: "orphan.txt", content: "unknown input\n", selected: map[string]eval.FileMatch{
			"two": {Path: "orphan.txt", Reason: "unknown-file"}, "other": {Path: "orphan.txt", Reason: "unknown-file"},
		}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for name, contents := range initial {
				writeFile(t, filepath.Join(root, name), contents)
			}
			gitRun(t, root, "add", ".")
			gitRun(t, root, "commit", "--allow-empty", "-m", "baseline")
			base := gitSHA(t, root, "HEAD")
			if tc.selected == nil {
				if got := nativeProgramOutput(t, root); got != "42\n" {
					t.Fatalf("baseline program = %q, want 42", got)
				}
			}
			writeFile(t, filepath.Join(root, tc.path), tc.content)
			gitRun(t, root, "add", ".")
			gitRun(t, root, "commit", "-m", "change input")
			head := gitSHA(t, root, "HEAD")
			want := tc.selected
			if want == nil {
				if got := nativeProgramOutput(t, root); got != "43\n" {
					t.Fatalf("changed program = %q, want 43", got)
				}
				want = map[string]eval.FileMatch{
					"one": {Path: tc.path, Reason: "go-dep"}, "two": {Path: tc.path, Reason: "go-dep"},
					"package": {Reason: "triggered-by", Rule: "one"},
				}
			}
			var stdout, stderr bytes.Buffer
			if code := run([]string{"--repo", root, "--json", "--verbose", base, head}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
			var results []eval.Result
			if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
				t.Fatal(err)
			}
			if len(results) != 5 {
				t.Fatalf("got %d targets, want 5: %s", len(results), stdout.String())
			}
			for _, result := range results {
				match, build := want[result.Target]
				files := []eval.FileMatch{}
				if build {
					files = append(files, match)
				}
				if result.Build != build || !reflect.DeepEqual(result.Files, files) {
					t.Errorf("%s: got %+v, want build=%t files=%+v", result.Target, result, build, files)
				}
			}
			if tc.selected == nil {
				stdout.Reset()
				stderr.Reset()
				if code := run([]string{"--repo", root, "--json", "--verbose", "--target", "one", base, head}, &stdout, &stderr); code != 0 {
					t.Fatalf("filtered exit %d: %s", code, stderr.String())
				}
				var filteredResults []eval.Result
				if err := json.Unmarshal(stdout.Bytes(), &filteredResults); err != nil {
					t.Fatal(err)
				}
				filtered := []eval.Result{
					{Target: "one", Build: true, Files: []eval.FileMatch{want["one"]}},
					{Target: "package", Build: true, Files: []eval.FileMatch{want["package"]}},
				}
				if !reflect.DeepEqual(filteredResults, filtered) {
					t.Errorf("filtered results = %+v, want %+v", filteredResults, filtered)
				}
			}
		})
	}
}

// --- Test helpers ---

func nativeProgramOutput(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("go", "run", "./cmd/one")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running native fixture: %v\n%s", err, out)
	}
	return string(out)
}

// --- End test helpers ---
