package config

import (
	"strings"
	"testing"
)

// TestParseValid verifies that a well-formed config parses without error
// and resolves defaults correctly.
func TestParseValid(t *testing.T) {
	yaml := `
global:
  ignore: ["docs/**"]
  trigger_all: ["go.mod"]
unknown_file: ignore
targets:
  api:
    path: ./cmd/api
    include: ["k8s/api.yaml"]
  web:
    lang: none
    include: ["web/**"]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.UnknownFile != "ignore" {
		t.Errorf("UnknownFile = %q, want %q", cfg.UnknownFile, "ignore")
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(cfg.Targets))
	}
	if lang := cfg.Targets["api"].ResolvedLang(); lang != "go" {
		t.Errorf("api.ResolvedLang() = %q, want %q", lang, "go")
	}
	if lang := cfg.Targets["web"].ResolvedLang(); lang != "none" {
		t.Errorf("web.ResolvedLang() = %q, want %q", lang, "none")
	}
}

// TestParseDefaultUnknownFile verifies that an omitted unknown_file field
// defaults to "trigger_all".
func TestParseDefaultUnknownFile(t *testing.T) {
	yaml := `
targets:
  api:
    path: ./cmd/api
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.UnknownFile != "trigger_all" {
		t.Errorf("UnknownFile = %q, want %q", cfg.UnknownFile, "trigger_all")
	}
}

// TestParseInvalidUnknownFile rejects unknown values for the unknown_file field.
func TestParseInvalidUnknownFile(t *testing.T) {
	yaml := `
unknown_file: maybe
targets:
  api:
    path: ./cmd/api
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "invalid value") {
		t.Fatalf("Parse err = %v, want invalid value error", err)
	}
}

// TestParseExplicitEmptyUnknownFile rejects an explicitly empty unknown_file.
// This catches the "empty-string-means-default" anti-pattern: if someone
// writes unknown_file: "", they get an error instead of a silent default.
func TestParseExplicitEmptyUnknownFile(t *testing.T) {
	yaml := `
unknown_file: ""
targets:
  api:
    path: ./cmd/api
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "invalid value") {
		t.Fatalf("Parse err = %v, want invalid value error for explicit empty string", err)
	}
}

// TestParseNoTargets rejects a config with zero targets.
func TestParseNoTargets(t *testing.T) {
	yaml := `
global:
  ignore: ["docs/**"]
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "at least one target") {
		t.Fatalf("Parse err = %v, want 'at least one target' error", err)
	}
}

// TestParseUnsupportedLang rejects target languages not yet implemented.
func TestParseUnsupportedLang(t *testing.T) {
	yaml := `
targets:
  api:
    path: ./cmd/api
    lang: python
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unsupported lang") {
		t.Fatalf("Parse err = %v, want unsupported lang error", err)
	}
}

// TestParseGoLangRequiresPath verifies that lang: go without a path is an error.
func TestParseGoLangRequiresPath(t *testing.T) {
	yaml := `
targets:
  api:
    lang: go
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "requires path") {
		t.Fatalf("Parse err = %v, want 'requires path' error", err)
	}
}

// TestParseNoneWithPathRejected verifies that lang: none with a path is
// rejected. The path would be silently ignored, which is a config error.
func TestParseNoneWithPathRejected(t *testing.T) {
	yaml := `
targets:
  web:
    lang: none
    path: ./cmd/web
    include: ["web/**"]
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("Parse err = %v, want incompatible error", err)
	}
}

// TestParseNoneNoRulesRejected verifies that a target with lang: none and
// no include/exclude patterns is rejected. Such a target has no rules and
// can only fire via global rules, which is almost certainly a config error.
func TestParseNoneNoRulesRejected(t *testing.T) {
	yaml := `
targets:
  empty:
    lang: none
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "no rules") {
		t.Fatalf("Parse err = %v, want 'no rules' error", err)
	}
}

// TestParseBadPattern verifies that syntactically invalid glob patterns
// are rejected at parse time (fail-fast on misconfiguration).
func TestParseBadPattern(t *testing.T) {
	yaml := `
targets:
  api:
    path: ./cmd/api
    include: ["[unclosed"]
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "invalid pattern") {
		t.Fatalf("Parse err = %v, want 'invalid pattern' error", err)
	}
}

// TestParseBadGlobalPattern verifies bad patterns in global.ignore are caught.
func TestParseBadGlobalPattern(t *testing.T) {
	yaml := `
global:
  ignore: ["[bad"]
targets:
  api:
    path: ./cmd/api
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "invalid pattern") {
		t.Fatalf("Parse err = %v, want 'invalid pattern' error", err)
	}
}

// TestParseStrictRejectsUnknownFields verifies that unknown YAML fields
// cause a parse error (fail-fast on misconfiguration).
func TestParseStrictRejectsUnknownFields(t *testing.T) {
	yaml := `
targets:
  api:
    path: ./cmd/api
    typo_field: oops
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("Parse succeeded, want error for unknown field")
	}
}

// TestResolvedLang verifies the lang resolution rules:
// explicit lang wins, then path implies "go", otherwise "none".
func TestResolvedLang(t *testing.T) {
	tests := []struct {
		name string
		t    Target
		want string
	}{
		{"explicit go", Target{Lang: "go", Path: "./cmd/api"}, "go"},
		{"explicit none with path", Target{Lang: "none", Path: "./cmd/api"}, "none"},
		{"implicit go from path", Target{Path: "./cmd/api"}, "go"},
		{"implicit none", Target{}, "none"},
		{"include only", Target{Include: []string{"web/**"}}, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.t.ResolvedLang(); got != tt.want {
				t.Errorf("ResolvedLang() = %q, want %q", got, tt.want)
			}
		})
	}
}
