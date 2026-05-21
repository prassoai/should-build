package eval

import (
	"testing"

	"github.com/prassoai/should-build/config"
)

// mustCfg builds a Config through config.Canonicalize so defaults (lang,
// unknown_file) match production exactly. Fails the test on invalid config.
func mustCfg(t *testing.T, g config.Global, unknownFile string, targets map[string]config.Target) *config.Config {
	t.Helper()
	cfg, err := config.Canonicalize(config.Config{
		Global:      g,
		UnknownFile: unknownFile,
		Targets:     targets,
	})
	if err != nil {
		t.Fatalf("invalid test config: %v", err)
	}
	return cfg
}

// TestGlobalIgnore verifies that globally ignored files never trigger any target.
func TestGlobalIgnore(t *testing.T) {
	c := mustCfg(t,
		config.Global{Ignore: []string{"docs/**", "**/*.md"}},
		"trigger_all",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	results := Evaluate(c, []string{"docs/design.txt", "README.md"}, nil, nil)
	if results[0].Build {
		t.Error("globally ignored files should not trigger any target")
	}
}

// TestTargetExclude verifies that a target's exclude patterns prevent triggering,
// even for files that match global.trigger_all.
func TestTargetExclude(t *testing.T) {
	c := mustCfg(t,
		config.Global{TriggerAll: []string{"go.mod", "go.sum"}},
		"trigger_all",
		map[string]config.Target{
			"api":    {Path: "./cmd/api"},
			"walker": {Path: "./cmd/walker", Exclude: []string{"go.mod", "go.sum"}},
		},
	)
	results := Evaluate(c, []string{"go.mod"}, nil, nil)
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
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"k8s/api.yaml"}},
			"web": {Include: []string{"web/**"}},
		},
	)
	results := Evaluate(c, []string{"k8s/api.yaml", "web/src/App.tsx"}, nil, nil)
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
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	deps := map[string][]string{
		"api": {"cmd/api/main.go", "internal/config/load.go"},
	}
	results := Evaluate(c, []string{"internal/config/load.go"}, deps, nil)
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
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	deps := map[string][]string{
		"api": {"cmd/api/main.go"},
	}
	results := Evaluate(c, []string{"internal/unrelated/x.go"}, deps, nil)
	if results[0].Build {
		t.Error("file outside dep graph should not trigger target (unknown_file=ignore)")
	}
}

// TestGlobalTriggerAll verifies that trigger_all patterns fire for non-excluded targets.
func TestGlobalTriggerAll(t *testing.T) {
	c := mustCfg(t,
		config.Global{TriggerAll: []string{"go.mod", "Makefile"}},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"web": {Include: []string{"web/**"}},
		},
	)
	results := Evaluate(c, []string{"Makefile"}, nil, nil)
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
	c := mustCfg(t,
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"web": {Include: []string{"web/**"}},
		},
	)
	results := Evaluate(c, []string{"random/new_file.xyz"}, nil, nil)
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
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	results := Evaluate(c, []string{"random/new_file.xyz"}, nil, nil)
	if results[0].Build {
		t.Error("unknown file should not trigger target when unknown_file=ignore")
	}
}

// TestExcludeBeatsInclude verifies that exclude is checked before include
// in the precedence chain. A file matching both exclude and include does
// not trigger the target.
func TestExcludeBeatsInclude(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {
				Include: []string{"config/**"},
				Exclude: []string{"config/test/**"},
			},
		},
	)
	results := Evaluate(c, []string{"config/test/fixture.yaml"}, nil, nil)
	if results[0].Build {
		t.Error("exclude should take precedence over include")
	}
}

// TestExcludeBeatsTriggerAll verifies that a target can exclude itself from
// global trigger_all files.
func TestExcludeBeatsTriggerAll(t *testing.T) {
	c := mustCfg(t,
		config.Global{TriggerAll: []string{"go.mod"}},
		"ignore",
		map[string]config.Target{
			"walker": {Exclude: []string{"go.mod"}},
		},
	)
	results := Evaluate(c, []string{"go.mod"}, nil, nil)
	if results[0].Build {
		t.Error("target excluding go.mod should not be triggered by trigger_all")
	}
}

// TestTargetTemplateExpansion verifies that {target} is expanded in include patterns.
func TestTargetTemplateExpansion(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"myservice": {Include: []string{"targets/{target}/conf/{target}-*.hjson"}},
		},
	)
	results := Evaluate(c, []string{"targets/myservice/conf/myservice-prod.hjson"}, nil, nil)
	if !results[0].Build {
		t.Error("{target} expansion in include should match")
	}
}

// TestTargetTemplateInExclude verifies {target} expansion works in exclude patterns.
func TestTargetTemplateInExclude(t *testing.T) {
	c := mustCfg(t,
		config.Global{TriggerAll: []string{"targets/**"}},
		"ignore",
		map[string]config.Target{
			"svc-a": {Exclude: []string{"targets/{target}/test/**"}},
		},
	)
	results := Evaluate(c, []string{"targets/svc-a/test/data.json"}, nil, nil)
	if results[0].Build {
		t.Error("{target} expansion in exclude should prevent trigger")
	}
}

// TestNoChangedFiles verifies that no changes means no rebuilds.
func TestNoChangedFiles(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
		},
	)
	results := Evaluate(c, nil, nil, nil)
	if results[0].Build {
		t.Error("no changed files should mean no rebuild")
	}
}

// TestMultipleFilesTriggerSameTarget verifies that multiple files can all
// contribute to triggering a single target.
func TestMultipleFilesTriggerSameTarget(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"k8s/**", "terraform/**"}},
		},
	)
	results := Evaluate(c, []string{"k8s/api.yaml", "terraform/main.tf"}, nil, nil)
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
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"shared/**"}},
			"web": {Include: []string{"shared/**"}},
		},
	)
	results := Evaluate(c, []string{"shared/util.go"}, nil, nil)
	for _, r := range results {
		if !r.Build {
			t.Errorf("target %q should be triggered by shared file", r.Target)
		}
	}
}

// TestSQLOnlyTriggersSQLTarget verifies that extension-scoped patterns
// trigger only the intended target, not others.
func TestSQLOnlyTriggersSQLTarget(t *testing.T) {
	c := mustCfg(t,
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
	results := Evaluate(c, []string{"db/migrations/001.sql"}, deps, nil)
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
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"zebra":  {Include: []string{"z/**"}},
			"alpha":  {Include: []string{"a/**"}},
			"middle": {Include: []string{"m/**"}},
		},
	)
	results := Evaluate(c, nil, nil, nil)
	if results[0].Target != "alpha" || results[1].Target != "middle" || results[2].Target != "zebra" {
		t.Errorf("results not sorted: %v, %v, %v", results[0].Target, results[1].Target, results[2].Target)
	}
}

// TestEmptyFilesSlice verifies that non-triggered targets have an empty
// (not nil) Files slice, so JSON encodes as [] not null.
func TestEmptyFilesSlice(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"api/**"}},
		},
	)
	results := Evaluate(c, []string{"unrelated.txt"}, nil, nil)
	if results[0].Files == nil {
		t.Error("Files should be [] not nil")
	}
}

// TestIncludeBeatsDepGraph verifies the precedence: include is checked
// before the dep graph, so the reason is "include" not "go-dep" when both match.
func TestIncludeBeatsDepGraph(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"api": {Path: "./cmd/api", Include: []string{"cmd/api/**"}},
		},
	)
	deps := map[string][]string{
		"api": {"cmd/api/main.go"},
	}
	results := Evaluate(c, []string{"cmd/api/main.go"}, deps, nil)
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
	c := mustCfg(t,
		config.Global{
			Ignore:     []string{"**/*.md"},
			TriggerAll: []string{"**/*.md"}, // contradicts ignore; ignore wins
		},
		"trigger_all",
		map[string]config.Target{
			"api": {Include: []string{"**/*.md"}}, // also matches; still ignored
		},
	)
	results := Evaluate(c, []string{"docs/README.md"}, nil, nil)
	if results[0].Build {
		t.Error("globally ignored file should not trigger any target")
	}
}

// TestTriggerPropagation verifies that when target A builds and triggers B,
// B is also marked as building with reason "triggered-by".
func TestTriggerPropagation(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"control": {Include: []string{"cmd/control/**"}, Triggers: []string{"vm"}},
			"vm":      {Include: []string{"cmd/vm/**"}},
		},
	)
	results := Evaluate(c, []string{"cmd/control/main.go"}, nil, nil)
	for _, r := range results {
		switch r.Target {
		case "control":
			if !r.Build {
				t.Error("control should build (include match)")
			}
		case "vm":
			if !r.Build {
				t.Error("vm should build (triggered by control)")
			}
			if len(r.Files) != 1 || r.Files[0].Reason != "triggered-by" {
				t.Errorf("vm reason = %v, want triggered-by", r.Files)
			}
			if r.Files[0].Rule != "control" {
				t.Errorf("vm trigger rule = %q, want %q", r.Files[0].Rule, "control")
			}
		}
	}
}

// TestTriggerTransitive verifies transitive propagation: A triggers B, B
// triggers C. When A builds, both B and C must also build.
func TestTriggerTransitive(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"a": {Include: []string{"a/**"}, Triggers: []string{"b"}},
			"b": {Include: []string{"b/**"}, Triggers: []string{"c"}},
			"c": {Include: []string{"c/**"}},
		},
	)
	results := Evaluate(c, []string{"a/x.go"}, nil, nil)
	for _, r := range results {
		if !r.Build {
			t.Errorf("target %q should build (transitive trigger from a)", r.Target)
		}
	}
	// Verify trigger chain: b triggered by a, c triggered by b.
	idx := make(map[string]Result, len(results))
	for _, r := range results {
		idx[r.Target] = r
	}
	if idx["b"].Files[0].Rule != "a" {
		t.Errorf("b should be triggered by a, got rule %q", idx["b"].Files[0].Rule)
	}
	if idx["c"].Files[0].Rule != "b" {
		t.Errorf("c should be triggered by b, got rule %q", idx["c"].Files[0].Rule)
	}
}

// TestTriggerNoOp verifies that triggers don't fire when the trigger source
// target wasn't going to build. No false positives.
func TestTriggerNoOp(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"control": {Include: []string{"cmd/control/**"}, Triggers: []string{"vm"}},
			"vm":      {Include: []string{"cmd/vm/**"}},
		},
	)
	// Change a file that doesn't match control's include.
	results := Evaluate(c, []string{"unrelated/file.txt"}, nil, nil)
	for _, r := range results {
		if r.Build {
			t.Errorf("target %q should not build — trigger source didn't build", r.Target)
		}
	}
}

// TestTriggerAlreadyBuilding verifies that a target already building from its
// own rules doesn't get a duplicate triggered-by entry.
func TestTriggerAlreadyBuilding(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"control": {Include: []string{"shared/**"}, Triggers: []string{"vm"}},
			"vm":      {Include: []string{"shared/**"}},
		},
	)
	results := Evaluate(c, []string{"shared/lib.go"}, nil, nil)
	for _, r := range results {
		if !r.Build {
			t.Errorf("target %q should build", r.Target)
		}
	}
	// vm should have its own include match, not a triggered-by entry.
	idx := make(map[string]Result, len(results))
	for _, r := range results {
		idx[r.Target] = r
	}
	for _, fm := range idx["vm"].Files {
		if fm.Reason == "triggered-by" {
			t.Error("vm already builds from include — should not have triggered-by entry")
		}
	}
}

// TestTriggerExpandsTargetFilter verifies that --target narrows initial
// evaluation but triggers expand the result set outward. When --target
// specifies only "control" and control triggers "vm", both targets must
// appear in the output: control builds from its own rules, vm builds
// because it was triggered.
func TestTriggerExpandsTargetFilter(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"control": {Include: []string{"cmd/control/**"}, Triggers: []string{"vm"}},
			"vm":      {Include: []string{"cmd/vm/**"}},
			"other":   {Include: []string{"other/**"}},
		},
	)
	// Only evaluate "control" initially, but vm should be pulled in via trigger.
	results := Evaluate(c, []string{"cmd/control/main.go"}, nil, []string{"control"})

	idx := make(map[string]Result, len(results))
	for _, r := range results {
		idx[r.Target] = r
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results (control + vm), got %d: %v", len(results), results)
	}
	if !idx["control"].Build {
		t.Error("control should build (include match)")
	}
	if !idx["vm"].Build {
		t.Error("vm should build (triggered by control)")
	}
	if _, ok := idx["other"]; ok {
		t.Error("other should not appear — not in --target and not triggered")
	}
}

// TestTriggerExpandsWithOwnRules verifies that a triggered target pulled in
// by --target expansion is fully evaluated against the diff. If its own rules
// match, those appear in Files (no triggered-by entry is added).
func TestTriggerExpandsWithOwnRules(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"control": {Include: []string{"cmd/control/**"}, Triggers: []string{"vm"}},
			"vm":      {Include: []string{"shared/**"}},
		},
	)
	// Both control and vm files changed, but only control is in --target.
	// vm should be pulled in via trigger AND have its own include match.
	results := Evaluate(c, []string{"cmd/control/main.go", "shared/lib.go"}, nil, []string{"control"})

	idx := make(map[string]Result, len(results))
	for _, r := range results {
		idx[r.Target] = r
	}

	if !idx["vm"].Build {
		t.Fatal("vm should build")
	}
	// vm builds from its own include rule — no triggered-by entry.
	for _, fm := range idx["vm"].Files {
		if fm.Reason == "triggered-by" {
			t.Error("vm builds from own rules — should not have triggered-by entry")
		}
	}
	if idx["vm"].Files[0].Reason != "include" {
		t.Errorf("vm reason = %q, want %q", idx["vm"].Files[0].Reason, "include")
	}
}

// TestTriggerMultipleSources verifies that when multiple targets trigger the
// same dependent, all trigger sources are recorded in the dependent's Files.
func TestTriggerMultipleSources(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"a":      {Include: []string{"a/**"}, Triggers: []string{"c"}},
			"b":      {Include: []string{"b/**"}, Triggers: []string{"c"}},
			"c":      {Include: []string{"c/**"}},
		},
	)
	// Both a and b build, both trigger c.
	results := Evaluate(c, []string{"a/x.go", "b/y.go"}, nil, nil)

	idx := make(map[string]Result, len(results))
	for _, r := range results {
		idx[r.Target] = r
	}

	if !idx["c"].Build {
		t.Fatal("c should build (triggered by a and b)")
	}
	if len(idx["c"].Files) != 2 {
		t.Fatalf("c should have 2 triggered-by entries, got %d: %v", len(idx["c"].Files), idx["c"].Files)
	}
	sources := map[string]bool{}
	for _, fm := range idx["c"].Files {
		if fm.Reason != "triggered-by" {
			t.Errorf("unexpected reason %q", fm.Reason)
		}
		sources[fm.Rule] = true
	}
	if !sources["a"] || !sources["b"] {
		t.Errorf("expected triggered-by from both a and b, got %v", sources)
	}
}

// TestRuleFieldPopulated verifies that the Rule field captures the matching pattern.
func TestRuleFieldPopulated(t *testing.T) {
	c := mustCfg(t,
		config.Global{TriggerAll: []string{"go.mod"}},
		"ignore",
		map[string]config.Target{
			"api": {Include: []string{"k8s/*.yaml"}},
		},
	)
	results := Evaluate(c, []string{"k8s/api.yaml", "go.mod"}, nil, nil)
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
