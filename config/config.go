package config

import (
	"bytes"
	"fmt"
	"os"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// Config is the top-level should-build.yaml schema.
type Config struct {
	Global      Global            `yaml:"global"`
	UnknownFile string            `yaml:"unknown_file"`
	Targets     map[string]Target `yaml:"targets"`
}

// Global defines rules that apply across all targets.
type Global struct {
	Ignore     []string `yaml:"ignore"`
	TriggerAll []string `yaml:"trigger_all"`
}

// Target defines build rules for a single component.
type Target struct {
	Path    string   `yaml:"path"`
	Lang    string   `yaml:"lang"`
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// ResolvedLang returns the effective language for the target.
// If Lang is explicitly set, it is returned as-is.
// Otherwise: "go" when Path is set, "none" when it is not.
func (t Target) ResolvedLang() string {
	if t.Lang != "" {
		return t.Lang
	}
	if t.Path != "" {
		return "go"
	}
	return "none"
}

// Load reads and validates a should-build.yaml at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	return Parse(data)
}

// Parse parses and validates a should-build.yaml from raw bytes.
func Parse(data []byte) (*Config, error) {
	// Decode into a raw type so we can distinguish absent unknown_file
	// (nil → default "trigger_all") from explicitly empty ("" → error).
	var raw struct {
		Global      Global            `yaml:"global"`
		UnknownFile *string           `yaml:"unknown_file"`
		Targets     map[string]Target `yaml:"targets"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg := &Config{
		Global:  raw.Global,
		Targets: raw.Targets,
	}
	switch {
	case raw.UnknownFile == nil:
		cfg.UnknownFile = "trigger_all"
	case *raw.UnknownFile == "trigger_all" || *raw.UnknownFile == "ignore":
		cfg.UnknownFile = *raw.UnknownFile
	default:
		return nil, fmt.Errorf("unknown_file: invalid value %q (want %q or %q)", *raw.UnknownFile, "trigger_all", "ignore")
	}

	return cfg, validate(cfg)
}

func validate(cfg *Config) error {
	if len(cfg.Targets) == 0 {
		return fmt.Errorf("config must define at least one target")
	}
	if err := validatePatterns("global.ignore", cfg.Global.Ignore); err != nil {
		return err
	}
	if err := validatePatterns("global.trigger_all", cfg.Global.TriggerAll); err != nil {
		return err
	}
	for name, t := range cfg.Targets {
		lang := t.ResolvedLang()
		switch lang {
		case "go":
			if t.Path == "" {
				return fmt.Errorf("target %q: lang %q requires path", name, lang)
			}
		case "none":
			if t.Path != "" {
				return fmt.Errorf("target %q: lang %q is incompatible with path (path is ignored when lang is none)", name, lang)
			}
			if len(t.Include) == 0 && len(t.Exclude) == 0 {
				return fmt.Errorf("target %q: lang %q with no include or exclude patterns has no rules", name, lang)
			}
		default:
			return fmt.Errorf("target %q: unsupported lang %q", name, t.Lang)
		}
		if err := validatePatterns(fmt.Sprintf("target %q include", name), t.Include); err != nil {
			return err
		}
		if err := validatePatterns(fmt.Sprintf("target %q exclude", name), t.Exclude); err != nil {
			return err
		}
	}
	return nil
}

// validatePatterns checks that every glob pattern is syntactically valid.
// A malformed pattern (e.g. unclosed character class) is a config error.
func validatePatterns(context string, patterns []string) error {
	for _, p := range patterns {
		if _, err := doublestar.Match(p, ""); err != nil {
			return fmt.Errorf("%s: invalid pattern %q: %w", context, p, err)
		}
	}
	return nil
}
