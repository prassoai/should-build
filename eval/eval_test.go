package eval

import (
	"testing"

	"github.com/prassoai/should-build/config"
)

// helper to find a result by target name.
func findResult(results []Result, target string) Result {
	for _, r := range results {
		if r.Target == target {
			return r
		}
	}
	return Result{}
}

// helper to check if a result has a file with the given reason.
func hasReason(r Result, reason string) bool {
	for _, f := range r.Files {
		if f.Reason == reason {
			return true
		}
	}
	return false
}

// TestGlobalIgnore verifies that files matching global.ignore are
// invisible to all targets (precedence rule 1).
func TestGlobalIgnore(t *testing.T) {
	cfg := &config.Config{
		Global:      config.Global{Ignore: []string{"**/*.md", ".github/**"}},
		UnknownFile: "trigger_all",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	}
	results := Evaluate(cfg, []string{"README.md", ".github/ci.yaml"}, nil)
	r := findResult(results, "api")
	if r.Build {
		t.Errorf("api should not build: globally ignored files only")
	}
}

// TestTargetExclude verifies that target.exclude blocks a file even
// if it matches global.trigger_all (precedence: exclude > trigger_all).
func TestTargetExclude(t *testing.T) {
	cfg := &config.Config{
		Global:      config.Global{TriggerAll: []string{"go.mod"}},
		UnknownFile: "trigger_all",
		Targets: map[string]config.Target{
			"walker": {Path: "./cmd/walker", Exclude: []string{"go.mod", "go.sum"}},
			"api":    {Path: "./cmd/api"},
		},
	}
	results := Evaluate(cfg, []string{"go.mod"}, nil)

	walker := findResult(results, "walker")
	if walker.Build {
		t.Error("walker should not build: go.mod is excluded")
	}

	api := findResult(results, "api")
	if !api.Build || !hasReason(api, "trigger-all") {
		t.Errorf("api should build via trigger-all, got build=%v", api.Build)
	}
}

// TestTargetInclude verifies that target.include triggers a rebuild
// (precedence rule 3).
func TestTargetInclude(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api", Include: []string{"k8s/api.yaml"}},
			"web": {Lang: "none", Include: []string{"web/**"}},
		},
	}
	changed := []string{"k8s/api.yaml", "web/src/App.tsx"}
	results := Evaluate(cfg, changed, nil)

	api := findResult(results, "api")
	if !api.Build || !hasReason(api, "include") {
		t.Errorf("api should build via include, got build=%v", api.Build)
	}

	web := findResult(results, "web")
	if !web.Build || !hasReason(web, "include") {
		t.Errorf("web should build via include, got build=%v", web.Build)
	}
}

// TestDepGraph verifies that files in a target's dependency graph
// trigger a rebuild (precedence rule 4).
func TestDepGraph(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"web": {Lang: "none", Include: []string{"web/**"}},
		},
	}
	deps := map[string][]string{
		"api": {"cmd/api/main.go", "internal/config/config.go"},
	}
	results := Evaluate(cfg, []string{"internal/config/config.go"}, deps)

	api := findResult(results, "api")
	if !api.Build || !hasReason(api, "go-dep") {
		t.Errorf("api should build via go-dep, got build=%v", api.Build)
	}

	web := findResult(results, "web")
	if web.Build {
		t.Error("web should not build: no include match, no dep graph")
	}
}

// TestTriggerAll verifies that files matching global.trigger_all trigger
// all non-excluded targets (precedence rule 5).
func TestTriggerAll(t *testing.T) {
	cfg := &config.Config{
		Global:      config.Global{TriggerAll: []string{"go.mod", "Makefile"}},
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"cli": {Path: "./cmd/cli"},
		},
	}
	results := Evaluate(cfg, []string{"Makefile"}, nil)
	for _, r := range results {
		if !r.Build || !hasReason(r, "trigger-all") {
			t.Errorf("%s should build via trigger-all, got build=%v", r.Target, r.Build)
		}
	}
}

// TestUnknownFileTriggerAll verifies that unmatched files trigger all
// targets when unknown_file is "trigger_all".
func TestUnknownFileTriggerAll(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "trigger_all",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	}
	results := Evaluate(cfg, []string{"mystery.xyz"}, nil)
	r := findResult(results, "api")
	if !r.Build || !hasReason(r, "unknown-file") {
		t.Errorf("api should build via unknown-file, got build=%v", r.Build)
	}
}

// TestUnknownFileIgnore verifies that unmatched files are silently
// skipped when unknown_file is "ignore".
func TestUnknownFileIgnore(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	}
	results := Evaluate(cfg, []string{"mystery.xyz"}, nil)
	r := findResult(results, "api")
	if r.Build {
		t.Error("api should not build: unknown file with ignore policy")
	}
}

// TestIncludeOverridesDepGraph verifies that include has higher precedence
// than dep-graph: a file matching both reports reason "include".
func TestIncludeOverridesDepGraph(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api", Include: []string{"cmd/api/**"}},
		},
	}
	deps := map[string][]string{
		"api": {"cmd/api/main.go"},
	}
	results := Evaluate(cfg, []string{"cmd/api/main.go"}, deps)
	r := findResult(results, "api")
	if !r.Build {
		t.Fatal("api should build")
	}
	if r.Files[0].Reason != "include" {
		t.Errorf("reason = %q, want %q (include has higher precedence)", r.Files[0].Reason, "include")
	}
}

// TestExcludeOverridesInclude verifies that exclude has higher precedence
// than include: a file matching both is excluded.
func TestExcludeOverridesInclude(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"api": {
				Path:    "./cmd/api",
				Include: []string{"k8s/**"},
				Exclude: []string{"k8s/debug.yaml"},
			},
		},
	}
	results := Evaluate(cfg, []string{"k8s/debug.yaml"}, nil)
	r := findResult(results, "api")
	if r.Build {
		t.Error("api should not build: exclude overrides include")
	}
}

// TestTemplateExpansion verifies that {target} in include/exclude patterns
// is expanded to the target name.
func TestTemplateExpansion(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"admin": {Lang: "none", Include: []string{"targets/{target}/conf/{target}-*.hjson"}},
			"api":   {Lang: "none", Include: []string{"targets/{target}/conf/{target}-*.hjson"}},
		},
	}
	results := Evaluate(cfg, []string{"targets/admin/conf/admin-nonprod.hjson"}, nil)

	admin := findResult(results, "admin")
	if !admin.Build {
		t.Error("admin should build: {target}=admin matches")
	}

	api := findResult(results, "api")
	if api.Build {
		t.Error("api should not build: {target}=api does not match admin's path")
	}
}

// TestEmptyChangedFiles verifies that no changes means no rebuilds.
func TestEmptyChangedFiles(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "trigger_all",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	}
	results := Evaluate(cfg, nil, nil)
	r := findResult(results, "api")
	if r.Build {
		t.Error("api should not build: no changed files")
	}
}

// TestResultsAreSorted verifies that results are returned in alphabetical
// order by target name for deterministic output.
func TestResultsAreSorted(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"zebra":    {Lang: "none", Include: []string{"z/**"}},
			"alpha":    {Lang: "none", Include: []string{"a/**"}},
			"middleee": {Lang: "none", Include: []string{"m/**"}},
		},
	}
	results := Evaluate(cfg, nil, nil)
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
	if results[0].Target != "alpha" || results[1].Target != "middleee" || results[2].Target != "zebra" {
		t.Errorf("results not sorted: %v, %v, %v", results[0].Target, results[1].Target, results[2].Target)
	}
}

// TestFilesNonNilInJSON verifies that Files is an empty slice (not nil)
// when no files trigger a target, so JSON serializes as [] not null.
func TestFilesNonNilInJSON(t *testing.T) {
	cfg := &config.Config{
		UnknownFile: "ignore",
		Targets: map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	}
	results := Evaluate(cfg, nil, nil)
	r := findResult(results, "api")
	if r.Files == nil {
		t.Error("Files should be non-nil empty slice, got nil")
	}
}
