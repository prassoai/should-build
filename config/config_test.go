package config

import (
	"strings"
	"testing"
)

// TestParseValid verifies a complete, valid config round-trips through Parse.
func TestParseValid(t *testing.T) {
	yaml := `
global:
  ignore:
    - "docs/**"
    - "**/*.md"
  trigger_all:
    - "go.mod"
unknown_file: ignore
targets:
  api:
    path: ./cmd/api
    include:
      - "k8s/api.yaml"
    exclude:
      - "go.sum"
  web:
    lang: none
    include:
      - "web/**"
  service:
    path: ./cmd/svc
    include:
      - "targets/{target}/conf/*.yaml"
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.UnknownFile != "ignore" {
		t.Errorf("UnknownFile = %q, want %q", cfg.UnknownFile, "ignore")
	}
	if len(cfg.Global.Ignore) != 2 {
		t.Errorf("Global.Ignore len = %d, want 2", len(cfg.Global.Ignore))
	}
	if len(cfg.Targets) != 3 {
		t.Errorf("Targets len = %d, want 3", len(cfg.Targets))
	}

	// Verify lang defaults.
	if cfg.Targets["api"].Lang != "go" {
		t.Errorf("api.Lang = %q, want %q", cfg.Targets["api"].Lang, "go")
	}
	if cfg.Targets["web"].Lang != "none" {
		t.Errorf("web.Lang = %q, want %q", cfg.Targets["web"].Lang, "none")
	}
}

// TestParseDefaultUnknownFile verifies empty unknown_file defaults to trigger_all.
func TestParseDefaultUnknownFile(t *testing.T) {
	yaml := `
targets:
  api:
    path: ./cmd/api
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.UnknownFile != "trigger_all" {
		t.Errorf("UnknownFile = %q, want %q", cfg.UnknownFile, "trigger_all")
	}
}

// TestParseDefaultLangWithPath verifies lang defaults to "go" when path is set.
func TestParseDefaultLangWithPath(t *testing.T) {
	yaml := `
targets:
  api:
    path: ./cmd/api
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Targets["api"].Lang != "go" {
		t.Errorf("Lang = %q, want %q", cfg.Targets["api"].Lang, "go")
	}
}

// TestParseDefaultLangNoPath verifies lang defaults to "none" when no path.
func TestParseDefaultLangNoPath(t *testing.T) {
	yaml := `
targets:
  web:
    include:
      - "web/**"
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Targets["web"].Lang != "none" {
		t.Errorf("Lang = %q, want %q", cfg.Targets["web"].Lang, "none")
	}
}

// TestParseInvalidUnknownFile rejects values other than trigger_all or ignore.
func TestParseInvalidUnknownFile(t *testing.T) {
	yaml := `
unknown_file: panic
targets:
  api:
    path: ./cmd/api
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid unknown_file")
	}
	if !strings.Contains(err.Error(), "unknown_file") {
		t.Errorf("error %q should mention unknown_file", err)
	}
}

// TestParseInvalidLang rejects unsupported lang values.
func TestParseInvalidLang(t *testing.T) {
	yaml := `
targets:
  api:
    lang: python
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid lang")
	}
	if !strings.Contains(err.Error(), "lang") {
		t.Errorf("error %q should mention lang", err)
	}
}

// TestParseNoTargets rejects configs with zero targets.
func TestParseNoTargets(t *testing.T) {
	yaml := `
global:
  ignore:
    - "docs/**"
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for no targets")
	}
	if !strings.Contains(err.Error(), "at least one target") {
		t.Errorf("error %q should mention targets", err)
	}
}

// TestParseInvalidPattern rejects malformed glob patterns.
func TestParseInvalidPattern(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "global ignore",
			yaml: `
targets:
  api:
    path: ./cmd/api
global:
  ignore:
    - "[bad"
`,
		},
		{
			name: "global trigger_all",
			yaml: `
targets:
  api:
    path: ./cmd/api
global:
  trigger_all:
    - "[bad"
`,
		},
		{
			name: "target include",
			yaml: `
targets:
  api:
    include:
      - "[bad"
`,
		},
		{
			name: "target exclude",
			yaml: `
targets:
  api:
    exclude:
      - "[bad"
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error for invalid pattern")
			}
		})
	}
}

// TestParseTriggersValid verifies that a target can declare triggers to other
// existing targets without error.
func TestParseTriggersValid(t *testing.T) {
	yaml := `
targets:
  control:
    lang: none
    include:
      - "cmd/control/**"
    triggers:
      - vm
      - cli
  vm:
    lang: none
    include:
      - "cmd/vm/**"
  cli:
    lang: none
    include:
      - "cmd/cli/**"
unknown_file: ignore
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Targets["control"].Triggers) != 2 {
		t.Errorf("Triggers len = %d, want 2", len(cfg.Targets["control"].Triggers))
	}
}

// TestParseTriggersUnknownTarget rejects triggers pointing to nonexistent targets.
func TestParseTriggersUnknownTarget(t *testing.T) {
	yaml := `
targets:
  api:
    lang: none
    triggers:
      - nonexistent
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for trigger to unknown target")
	}
	if !strings.Contains(err.Error(), "unknown target") {
		t.Errorf("error %q should mention unknown target", err)
	}
}

// TestParseTriggersSelfReference rejects a target that triggers itself.
func TestParseTriggersSelfReference(t *testing.T) {
	yaml := `
targets:
  api:
    lang: none
    triggers:
      - api
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for self-triggering target")
	}
	if !strings.Contains(err.Error(), "triggers itself") {
		t.Errorf("error %q should mention self-trigger", err)
	}
}

// TestParseTriggersCycleDetection rejects configs with cycles in the trigger
// graph. Cycles are a config error — they must be caught at parse time, not
// at evaluation time where they'd cause infinite loops.
//
// The exact cycle path is asserted to catch regressions in the DFS parent-walk
// and reverse logic. The outer DFS loop iterates in sorted order, so the path
// is deterministic.
func TestParseTriggersCycleDetection(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "direct cycle A->B->A",
			yaml: `
targets:
  a:
    lang: none
    triggers: [b]
  b:
    lang: none
    triggers: [a]
`,
			wantErr: "trigger cycle: a -> b -> a",
		},
		{
			name: "transitive cycle A->B->C->A",
			yaml: `
targets:
  a:
    lang: none
    triggers: [b]
  b:
    lang: none
    triggers: [c]
  c:
    lang: none
    triggers: [a]
`,
			wantErr: "trigger cycle: a -> b -> c -> a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error for trigger cycle")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestParseDefaultSelection verifies that a target with no selection field
// defaults to fail_open. Every config written before the field existed must
// keep selecting on global.trigger_all and the unknown_file fallback.
func TestParseDefaultSelection(t *testing.T) {
	yaml := `
targets:
  api:
    path: ./cmd/api
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Targets["api"].Selection != SelectionFailOpen {
		t.Errorf("Selection = %q, want %q", cfg.Targets["api"].Selection, SelectionFailOpen)
	}
}

// TestParseSelectionExplicit verifies that a target can opt into explicit
// selection, and that doing so leaves its neighbours fail-open. The mode is
// per-target: opting one expensive target out of the repo-wide rules must not
// change how anything else is selected.
func TestParseSelectionExplicit(t *testing.T) {
	yaml := `
targets:
  base-images:
    lang: none
    selection: explicit
    include:
      - "terraform/packer/**"
  api:
    path: ./cmd/api
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got := cfg.Targets["base-images"].Selection; got != SelectionExplicit {
		t.Errorf("base-images Selection = %q, want %q", got, SelectionExplicit)
	}
	if got := cfg.Targets["api"].Selection; got != SelectionFailOpen {
		t.Errorf("api Selection = %q, want %q", got, SelectionFailOpen)
	}
}

// TestParseInvalidSelection rejects selection values other than the two
// defined modes. A typo such as "explict" must not silently fall back to
// fail-open and quietly resume the over-building the mode exists to stop.
func TestParseInvalidSelection(t *testing.T) {
	yaml := `
targets:
  api:
    path: ./cmd/api
    selection: explict
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid selection")
	}
	if !strings.Contains(err.Error(), "selection") {
		t.Errorf("error %q should mention selection", err)
	}
}

// TestParseExplicitUnselectable rejects an explicit target that no rule could
// ever select. Fail-open's failure mode is a wasted build; explicit's is a
// target that silently never builds again and ships stale artifacts, so an
// explicit target with no include patterns, no dependency graph and no
// incoming trigger must fail loudly at parse time.
func TestParseExplicitUnselectable(t *testing.T) {
	yaml := `
targets:
  base-images:
    lang: none
    selection: explicit
  api:
    path: ./cmd/api
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unselectable explicit target")
	}
	if !strings.Contains(err.Error(), "base-images") || !strings.Contains(err.Error(), "nothing can select it") {
		t.Errorf("error %q should name the target and say nothing can select it", err)
	}
}

// TestParseExplicitSelectableWithoutInclude verifies the two non-include ways
// an explicit target can still be selected — a Go dependency graph, and an
// incoming trigger from another target — are both accepted. Neither is a
// repo-wide fallback, so neither contradicts the mode.
func TestParseExplicitSelectableWithoutInclude(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "go dependency graph",
			yaml: `
targets:
  vm:
    path: ./cmd/vm
    selection: explicit
`,
		},
		{
			name: "incoming trigger",
			yaml: `
targets:
  control:
    lang: none
    include:
      - "cmd/control/**"
    triggers: [vm]
  vm:
    lang: none
    selection: explicit
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.yaml)); err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
		})
	}
}

// TestParseExplicitPathWithoutDepGraph rejects an explicit target whose path
// is set but whose lang is "none": no analyzer runs, so the path selects
// nothing and the target is as unselectable as one with no inputs at all.
func TestParseExplicitPathWithoutDepGraph(t *testing.T) {
	yaml := `
targets:
  vm:
    path: ./cmd/vm
    lang: none
    selection: explicit
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error: lang none means the path builds no dependency graph")
	}
}

// TestParseInvalidYAML rejects syntactically broken YAML.
func TestParseInvalidYAML(t *testing.T) {
	_, err := Parse([]byte("{{{{"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// TestParseEmptyConfig rejects truly empty input.
func TestParseEmptyConfig(t *testing.T) {
	_, err := Parse([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
