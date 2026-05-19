package eval

import (
	"testing"

	"github.com/prassoai/should-build/config"
)

// helper builds a Config with defaults applied so tests don't need boilerplate.
func cfg(g config.Global, unknownFile string, targets map[string]config.Target) *config.Config {
	if unknownFile == "" {
		unknownFile = "trigger_all"
	}
	for name, t := range targets {
		if t.Lang == "" {
			if t.Path != "" {
				t.Lang = "go"
			} else {
				t.Lang = "none"
			}
		}
		targets[name] = t
	}
	return &config.Config{
		Global:      g,
		UnknownFile: unknownFile,
		Targets:     targets,
	}
}

// TestGlobalIgnore verifies that globally ignored files never trigger any target.
func TestGlobalIgnore(t *testing.T) {
	c := cfg(
		config.Global{Ignore: []string{"docs/**", "**/*.md"}},
		"trigger_all",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	results := Evaluate(c, []string{"docs/design.txt", "README.md"}, nil)
	if results[0].Build {
		t.Error("globally ignored files should not trigger any target")
	}
}

// TestTargetExclude verifies that a target's exclude patterns prevent triggering,
// even for files that match global.trigger_all.
func TestTargetExclude(t *testing.T) {
	c := cfg(
		config.Global{TriggerAll: []string{"go.mod", "go.sum"}},
		"trigger_all",
		map[string]config.Target{
			"api":    {Path: "./cmd/api"},
			"walker": {Path: "./cmd/walker", Exclude: []string{"go.mod", "go.sum"}},
		},
	)
	results := Evaluate(c, []string{"go.mod"}, nil)
	for _, r := range results {
		switch r.Target {
		case "api":
			if !r.Build {
				t.Error("api should be triggered by go.mod via trigger_all")
			}
		case "walker":
			if r.Build {
				t.Error("walker should be excluded from go.mod trigger")
			}
		}
	}
}

// TestTargetInclude verifies that include patterns trigger the target.
func TestTargetInclude(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"k8s/api.yaml"}},
			"web": {Include: []string{"web/**"}},
		},
	)
	results := Evaluate(c, []string{"k8s/api.yaml", "web/src/App.tsx"}, nil)
	for _, r := range results {
		if !r.Build {
			t.Errorf("target %q should be triggered by include", r.Target)
		}
		if r.Files[0].Reason != "include" {
			t.Errorf("target %q reason = %q, want %q", r.Target, r.Files[0].Reason, "include")
		}
	}
}

// TestDepGraph verifies that files in the dependency graph trigger the target.
func TestDepGraph(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	deps := map[string][]string{
		"api": {"cmd/api/main.go", "internal/config/load.go"},
	}
	results := Evaluate(c, []string{"internal/config/load.go"}, deps)
	if !results[0].Build {
		t.Error("file in dep graph should trigger target")
	}
	if results[0].Files[0].Reason != "go-dep" {
		t.Errorf("reason = %q, want %q", results[0].Files[0].Reason, "go-dep")
	}
}

// TestDepGraphNoMatch verifies that files outside the dep graph don't trigger
// via the dep graph path (they may still trigger via other rules).
func TestDepGraphNoMatch(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	deps := map[string][]string{
		"api": {"cmd/api/main.go"},
	}
	results := Evaluate(c, []string{"internal/unrelated/x.go"}, deps)
	if results[0].Build {
		t.Error("file outside dep graph should not trigger target (unknown_file=ignore)")
	}
}

// TestGlobalTriggerAll verifies that trigger_all patterns fire for non-excluded targets.
func TestGlobalTriggerAll(t *testing.T) {
	c := cfg(
		config.Global{TriggerAll: []string{"go.mod", "Makefile"}},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"web": {},
		},
	)
	results := Evaluate(c, []string{"Makefile"}, nil)
	for _, r := range results {
		if !r.Build {
			t.Errorf("target %q should be triggered by trigger_all", r.Target)
		}
		if r.Files[0].Reason != "trigger-all" {
			t.Errorf("target %q reason = %q, want %q", r.Target, r.Files[0].Reason, "trigger-all")
		}
	}
}

// TestUnknownFileTriggerAll verifies that unmatched files rebuild everything
// when unknown_file is "trigger_all".
func TestUnknownFileTriggerAll(t *testing.T) {
	c := cfg(
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"web": {},
		},
	)
	results := Evaluate(c, []string{"random/new_file.xyz"}, nil)
	for _, r := range results {
		if !r.Build {
			t.Errorf("target %q should be triggered by unknown file (trigger_all)", r.Target)
		}
		if r.Files[0].Reason != "unknown-file" {
			t.Errorf("target %q reason = %q, want %q", r.Target, r.Files[0].Reason, "unknown-file")
		}
	}
}

// TestUnknownFileIgnore verifies that unmatched files are silently skipped
// when unknown_file is "ignore".
func TestUnknownFileIgnore(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	results := Evaluate(c, []string{"random/new_file.xyz"}, nil)
	if results[0].Build {
		t.Error("unknown file should not trigger target when unknown_file=ignore")
	}
}

// TestExcludeBeatsInclude verifies that exclude is checked before include
// in the precedence chain. A file matching both exclude and include does
// not trigger the target.
func TestExcludeBeatsInclude(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {
				Include: []string{"config/**"},
				Exclude: []string{"config/test/**"},
			},
		},
	)
	results := Evaluate(c, []string{"config/test/fixture.yaml"}, nil)
	if results[0].Build {
		t.Error("exclude should take precedence over include")
	}
}

// TestExcludeBeatsTriggerAll verifies that a target can exclude itself from
// global trigger_all files.
func TestExcludeBeatsTriggerAll(t *testing.T) {
	c := cfg(
		config.Global{TriggerAll: []string{"go.mod"}},
		"ignore",
		map[string]config.Target{
			"walker": {Exclude: []string{"go.mod"}},
		},
	)
	results := Evaluate(c, []string{"go.mod"}, nil)
	if results[0].Build {
		t.Error("target excluding go.mod should not be triggered by trigger_all")
	}
}

// TestTargetTemplateExpansion verifies that {target} is expanded in include patterns.
func TestTargetTemplateExpansion(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"myservice": {Include: []string{"targets/{target}/conf/{target}-*.hjson"}},
		},
	)
	results := Evaluate(c, []string{"targets/myservice/conf/myservice-prod.hjson"}, nil)
	if !results[0].Build {
		t.Error("{target} expansion in include should match")
	}
}

// TestTargetTemplateInExclude verifies {target} expansion works in exclude patterns.
func TestTargetTemplateInExclude(t *testing.T) {
	c := cfg(
		config.Global{TriggerAll: []string{"targets/**"}},
		"ignore",
		map[string]config.Target{
			"svc-a": {Exclude: []string{"targets/{target}/test/**"}},
		},
	)
	results := Evaluate(c, []string{"targets/svc-a/test/data.json"}, nil)
	if results[0].Build {
		t.Error("{target} expansion in exclude should prevent trigger")
	}
}

// TestNoChangedFiles verifies that no changes means no rebuilds.
func TestNoChangedFiles(t *testing.T) {
	c := cfg(
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	results := Evaluate(c, nil, nil)
	if results[0].Build {
		t.Error("no changed files should mean no rebuild")
	}
}

// TestMultipleFilesTriggerSameTarget verifies that multiple files can all
// contribute to triggering a single target.
func TestMultipleFilesTriggerSameTarget(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"k8s/**", "terraform/**"}},
		},
	)
	results := Evaluate(c, []string{"k8s/api.yaml", "terraform/main.tf"}, nil)
	if !results[0].Build {
		t.Error("multiple matching files should trigger target")
	}
	if len(results[0].Files) != 2 {
		t.Errorf("Files count = %d, want 2", len(results[0].Files))
	}
}

// TestSameFileTriggersDifferentTargets verifies that one file can trigger
// multiple targets through different rules.
func TestSameFileTriggersDifferentTargets(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"shared/**"}},
			"web": {Include: []string{"shared/**"}},
		},
	)
	results := Evaluate(c, []string{"shared/util.go"}, nil)
	for _, r := range results {
		if !r.Build {
			t.Errorf("target %q should be triggered by shared file", r.Target)
		}
	}
}

// TestSQLOnlyTriggersSQLTarget verifies that extension-scoped patterns
// trigger only the intended target, not others.
func TestSQLOnlyTriggersSQLTarget(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"sql": {Include: []string{"**/*.sql"}},
		},
	)
	deps := map[string][]string{
		"api": {"cmd/api/main.go"},
	}
	results := Evaluate(c, []string{"db/migrations/001.sql"}, deps)
	for _, r := range results {
		switch r.Target {
		case "sql":
			if !r.Build {
				t.Error("sql target should be triggered by .sql file")
			}
		case "api":
			if r.Build {
				t.Error("api target should not be triggered by .sql file")
			}
		}
	}
}

// TestDeterministicOrder verifies that results are sorted by target name.
func TestDeterministicOrder(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"zebra":    {},
			"alpha":    {},
			"middle":   {},
		},
	)
	results := Evaluate(c, nil, nil)
	if results[0].Target != "alpha" || results[1].Target != "middle" || results[2].Target != "zebra" {
		t.Errorf("results not sorted: %v, %v, %v", results[0].Target, results[1].Target, results[2].Target)
	}
}

// TestEmptyFilesSlice verifies that non-triggered targets have an empty
// (not nil) Files slice, so JSON encodes as [] not null.
func TestEmptyFilesSlice(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {},
		},
	)
	results := Evaluate(c, []string{"unrelated.txt"}, nil)
	if results[0].Files == nil {
		t.Error("Files should be [] not nil")
	}
}

// TestIncludeBeatsDepGraph verifies the precedence: include is checked
// before the dep graph, so the reason is "include" not "go-dep" when both match.
func TestIncludeBeatsDepGraph(t *testing.T) {
	c := cfg(
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api", Include: []string{"cmd/api/**"}},
		},
	)
	deps := map[string][]string{
		"api": {"cmd/api/main.go"},
	}
	results := Evaluate(c, []string{"cmd/api/main.go"}, deps)
	if !results[0].Build {
		t.Fatal("should be triggered")
	}
	if results[0].Files[0].Reason != "include" {
		t.Errorf("reason = %q, want %q (include beats dep graph)", results[0].Files[0].Reason, "include")
	}
}

// TestGlobalIgnoreBeatsEverything verifies that a globally ignored file
// doesn't trigger any target, even if it matches include or trigger_all.
func TestGlobalIgnoreBeatsEverything(t *testing.T) {
	c := cfg(
		config.Global{
			Ignore:     []string{"**/*.md"},
			TriggerAll: []string{"**/*.md"}, // contradicts ignore; ignore wins
		},
		"trigger_all",
		map[string]config.Target{
			"api": {Include: []string{"**/*.md"}}, // also matches; still ignored
		},
	)
	results := Evaluate(c, []string{"docs/README.md"}, nil)
	if results[0].Build {
		t.Error("globally ignored file should not trigger any target")
	}
}

// TestRuleFieldPopulated verifies that the Rule field captures the matching pattern.
func TestRuleFieldPopulated(t *testing.T) {
	c := cfg(
		config.Global{TriggerAll: []string{"go.mod"}},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"k8s/*.yaml"}},
		},
	)
	results := Evaluate(c, []string{"k8s/api.yaml", "go.mod"}, nil)
	if !results[0].Build {
		t.Fatal("should be triggered")
	}
	for _, fm := range results[0].Files {
		switch fm.Reason {
		case "include":
			if fm.Rule != "k8s/*.yaml" {
				t.Errorf("include rule = %q, want %q", fm.Rule, "k8s/*.yaml")
			}
		case "trigger-all":
			if fm.Rule != "go.mod" {
				t.Errorf("trigger-all rule = %q, want %q", fm.Rule, "go.mod")
			}
		}
	}
}
