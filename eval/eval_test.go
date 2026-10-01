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

// TestUnknownFileScopedToDependingTarget locks in the central no-over-build
// requirement: a changed file that lives in one Go target's import closure
// (and no other's) rebuilds only that target — even under unknown_file=
// trigger_all. The dep graph correctly attributes the file to its owner via
// "go-dep"; the bug was that the same file looked "unknown" to every other
// target, so trigger_all rebuilt all of them. A file some target depends on is
// not an orphan, so the unknown-file fallback must not fire for targets that
// don't depend on it. This mirrors the real CI regression where a change to
// internal packages imported by murmur-api/murmur-control rebuilt murmur-ui.
func TestUnknownFileScopedToDependingTarget(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"ui":  {Path: "./cmd/ui"},
		},
	)
	// internal/server/handler.go is in api's import closure but not ui's.
	deps := map[string][]string{
		"api": {"cmd/api/main.go", "internal/server/handler.go"},
		"ui":  {"cmd/ui/main.go", "gen/ui/status.go"},
	}
	results := Evaluate(c, []string{"internal/server/handler.go"}, deps, nil)

	idx := byTarget(results)
	if !idx["api"].Build {
		t.Error("api should build — the changed file is in its import closure")
	}
	if idx["api"].Files[0].Reason != "go-dep" {
		t.Errorf("api reason = %q, want %q", idx["api"].Files[0].Reason, "go-dep")
	}
	if idx["ui"].Build {
		t.Error("ui should NOT build — it does not depend on the changed file; a file owned by another target is not an orphan")
	}
}

// TestUnknownFileOrphanStillTriggersAll verifies the safety net is preserved:
// a file no target accounts for (not in any dep graph, not matched by any
// include) still triggers every target under trigger_all — even when the diff
// also contains files that are claimed by specific targets. Erring on the side
// of rebuilding for genuinely unknown files is the whole point of trigger_all.
func TestUnknownFileOrphanStillTriggersAll(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"api": {Path: "./cmd/api"},
			"ui":  {Path: "./cmd/ui"},
		},
	)
	deps := map[string][]string{
		"api": {"cmd/api/main.go"},
		"ui":  {"cmd/ui/main.go"},
	}
	// mystery.cfg is in nobody's import closure → genuine orphan.
	results := Evaluate(c, []string{"mystery.cfg"}, deps, nil)
	for _, r := range results {
		if !r.Build {
			t.Errorf("target %q should build — orphan file triggers all", r.Target)
		}
		if r.Files[0].Reason != "unknown-file" {
			t.Errorf("target %q reason = %q, want %q", r.Target, r.Files[0].Reason, "unknown-file")
		}
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
	idx := byTarget(results)
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
	idx := byTarget(results)
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

	idx := byTarget(results)

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

	idx := byTarget(results)

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
			"a": {Include: []string{"a/**"}, Triggers: []string{"c"}},
			"b": {Include: []string{"b/**"}, Triggers: []string{"c"}},
			"c": {Include: []string{"c/**"}},
		},
	)
	// Both a and b build, both trigger c.
	results := Evaluate(c, []string{"a/x.go", "b/y.go"}, nil, nil)

	idx := byTarget(results)

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

// byTarget indexes results for assertions that name specific targets.
func byTarget(results []Result) map[string]Result {
	idx := make(map[string]Result, len(results))
	for _, r := range results {
		idx[r.Target] = r
	}
	return idx
}

// TestExplicitSelectionIgnoresTriggerAll is the central requirement of the
// explicit mode: global.trigger_all names no target's inputs, so it must not
// select an explicit one. This is the real regression — a go.mod bump, or a
// touched .github workflow, repeatedly launched a half-hour three-cloud image
// bake that no change to the image recipe had asked for.
func TestExplicitSelectionIgnoresTriggerAll(t *testing.T) {
	c := mustCfg(t,
		config.Global{TriggerAll: []string{"go.mod", ".github/**"}},
		"ignore",
		map[string]config.Target{
			"api":         {Path: "./cmd/api"},
			"base-images": {Selection: config.SelectionExplicit, Include: []string{"terraform/packer/**"}},
		},
	)
	idx := byTarget(Evaluate(c, []string{"go.mod", ".github/workflows/deploy.yaml"}, nil, nil))

	if !idx["api"].Build {
		t.Error("api should build — fail-open targets still select on trigger_all")
	}
	if idx["base-images"].Build {
		t.Errorf("base-images must not build: trigger_all names none of its inputs, got %v", idx["base-images"].Files)
	}
}

// TestExplicitSelectionIgnoresUnknownFile verifies that the unknown_file
// fallback never reaches an explicit target. An orphan — a file no target
// accounts for, such as a new top-level directory — is precisely the case the
// fallback exists to over-build for, and precisely the case an expensive
// target must not pay for.
func TestExplicitSelectionIgnoresUnknownFile(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"api":         {Path: "./cmd/api"},
			"base-images": {Selection: config.SelectionExplicit, Include: []string{"terraform/packer/**"}},
		},
	)
	deps := map[string][]string{"api": {"cmd/api/main.go"}}
	idx := byTarget(Evaluate(c, []string{"native/App.swift"}, deps, nil))

	if !idx["api"].Build {
		t.Error("api should build — orphan files still trigger fail-open targets")
	}
	if idx["api"].Files[0].Reason != "unknown-file" {
		t.Errorf("api reason = %q, want %q", idx["api"].Files[0].Reason, "unknown-file")
	}
	if idx["base-images"].Build {
		t.Errorf("base-images must not build from an orphan file, got %v", idx["base-images"].Files)
	}
}

// TestExplicitSelectionInclude verifies the mode is an opt-out from the
// repo-wide rules only: a file matching the target's own include patterns
// still selects it, with the ordinary "include" reason. Without this the mode
// would be indistinguishable from deleting the target.
func TestExplicitSelectionInclude(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"base-images": {Selection: config.SelectionExplicit, Include: []string{"terraform/packer/**"}},
		},
	)
	results := Evaluate(c, []string{"terraform/packer/gce.pkr.hcl"}, nil, nil)
	if !results[0].Build {
		t.Fatal("base-images should build — the changed file matches its include")
	}
	if results[0].Files[0].Reason != "include" {
		t.Errorf("reason = %q, want %q", results[0].Files[0].Reason, "include")
	}
	if results[0].Files[0].Rule != "terraform/packer/**" {
		t.Errorf("rule = %q, want %q", results[0].Files[0].Rule, "terraform/packer/**")
	}
}

// TestExplicitSelectionExclude verifies that exclude still carves holes in an
// explicit target's include patterns. The recipe's own tests and docs live
// under the recipe directory but cannot change what a baked image contains,
// so excluding them must leave the target unselected rather than fall through
// to the unknown_file fallback — the file is claimed, not orphaned.
func TestExplicitSelectionExclude(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"base-images": {
				Selection: config.SelectionExplicit,
				Include:   []string{"terraform/packer/**"},
				Exclude:   []string{"terraform/packer/tests/**", "terraform/packer/**/*.md"},
			},
		},
	)
	results := Evaluate(c, []string{"terraform/packer/tests/smoke_test.sh", "terraform/packer/README.md"}, nil, nil)
	if results[0].Build {
		t.Errorf("excluded recipe files must not select base-images, got %v", results[0].Files)
	}
}

// TestExplicitSelectionDepGraph verifies that a language dependency graph
// still selects an explicit target. A Go import closure names the target's
// inputs exactly — it is the opposite of a repo-wide fallback — so the mode
// has no reason to suppress it.
func TestExplicitSelectionDepGraph(t *testing.T) {
	c := mustCfg(t,
		config.Global{TriggerAll: []string{"go.mod"}},
		"trigger_all",
		map[string]config.Target{
			"vm": {Path: "./cmd/vm", Selection: config.SelectionExplicit},
		},
	)
	deps := map[string][]string{"vm": {"cmd/vm/main.go", "internal/boot/boot.go"}}
	results := Evaluate(c, []string{"internal/boot/boot.go"}, deps, nil)
	if !results[0].Build {
		t.Fatal("vm should build — the changed file is in its import closure")
	}
	if results[0].Files[0].Reason != "go-dep" {
		t.Errorf("reason = %q, want %q", results[0].Files[0].Reason, "go-dep")
	}
}

// TestExplicitSelectionStillTriggered verifies that trigger propagation still
// reaches an explicit target. A triggers edge names the target outright — it
// is a deliberate co-build declaration, not a repo-wide fallback — so a
// control-plane change that must ship with a matching image still bakes one.
func TestExplicitSelectionStillTriggered(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"ignore",
		map[string]config.Target{
			"control":     {Include: []string{"cmd/control/**"}, Triggers: []string{"base-images"}},
			"base-images": {Selection: config.SelectionExplicit, Include: []string{"terraform/packer/**"}},
		},
	)
	idx := byTarget(Evaluate(c, []string{"cmd/control/main.go"}, nil, nil))
	if !idx["base-images"].Build {
		t.Fatal("base-images should build — control triggers it")
	}
	if idx["base-images"].Files[0].Reason != "triggered-by" || idx["base-images"].Files[0].Rule != "control" {
		t.Errorf("base-images files = %v, want triggered-by control", idx["base-images"].Files)
	}
}

// TestExplicitSelectionClaimsItsFiles verifies that an explicit target still
// claims its include patterns for orphan classification. Declaring an
// expensive target's inputs must also tell every other target those files are
// accounted for; otherwise a recipe change would orphan-trigger all 25 other
// targets while the one target that owns it builds for the right reason.
func TestExplicitSelectionClaimsItsFiles(t *testing.T) {
	c := mustCfg(t,
		config.Global{},
		"trigger_all",
		map[string]config.Target{
			"api":         {Path: "./cmd/api"},
			"base-images": {Selection: config.SelectionExplicit, Include: []string{"terraform/packer/**"}},
		},
	)
	deps := map[string][]string{"api": {"cmd/api/main.go"}}
	idx := byTarget(Evaluate(c, []string{"terraform/packer/gce.pkr.hcl"}, deps, nil))

	if !idx["base-images"].Build {
		t.Error("base-images should build — the recipe changed")
	}
	if idx["api"].Build {
		t.Errorf("api must not build: the recipe file is claimed by base-images, not an orphan, got %v", idx["api"].Files)
	}
}

// TestGlobalIgnoreAppliesToExplicitTarget verifies that global.ignore is
// unchanged by the mode. It is a repo-wide rule, but it only ever suppresses
// builds, so an explicit target has no reason to opt out of it — and a file
// ignored globally must stay invisible to every target in either mode.
func TestGlobalIgnoreAppliesToExplicitTarget(t *testing.T) {
	c := mustCfg(t,
		config.Global{Ignore: []string{"**/*.md"}},
		"trigger_all",
		map[string]config.Target{
			"base-images": {Selection: config.SelectionExplicit, Include: []string{"terraform/packer/**"}},
		},
	)
	results := Evaluate(c, []string{"terraform/packer/README.md"}, nil, nil)
	if results[0].Build {
		t.Errorf("globally ignored file must not select an explicit target, got %v", results[0].Files)
	}
}

// TestFailOpenUnchangedByExplicitNeighbour locks the no-regression
// requirement: adding an explicit target to a config must not alter how any
// other target is selected. One diff, every fail-open route — trigger_all,
// orphan fallback, include and dep graph — still fires for the fail-open
// targets while the explicit one stays unselected.
func TestFailOpenUnchangedByExplicitNeighbour(t *testing.T) {
	targets := map[string]config.Target{
		"api": {Path: "./cmd/api"},
		"web": {Include: []string{"web/**"}},
	}
	deps := map[string][]string{"api": {"cmd/api/main.go"}}
	changed := []string{"go.mod", "native/App.swift", "web/src/App.tsx", "cmd/api/main.go"}

	base := byTarget(Evaluate(mustCfg(t, config.Global{TriggerAll: []string{"go.mod"}}, "trigger_all", targets), changed, deps, nil))

	withExplicit := make(map[string]config.Target, len(targets)+1)
	for name, tgt := range targets {
		withExplicit[name] = tgt
	}
	withExplicit["base-images"] = config.Target{Selection: config.SelectionExplicit, Include: []string{"terraform/packer/**"}}
	got := byTarget(Evaluate(mustCfg(t, config.Global{TriggerAll: []string{"go.mod"}}, "trigger_all", withExplicit), changed, deps, nil))

	for _, name := range []string{"api", "web"} {
		if !got[name].Build {
			t.Errorf("%s should still build", name)
		}
		if len(got[name].Files) != len(base[name].Files) {
			t.Errorf("%s files changed: %v, want %v", name, got[name].Files, base[name].Files)
		}
		for i, fm := range got[name].Files {
			if fm != base[name].Files[i] {
				t.Errorf("%s file %d = %v, want %v", name, i, fm, base[name].Files[i])
			}
		}
	}
	if got["base-images"].Build {
		t.Errorf("base-images must not build from this diff, got %v", got["base-images"].Files)
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
