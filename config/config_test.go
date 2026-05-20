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
