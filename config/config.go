package config

import (
	"fmt"
	"os"

	"github.com/prassoai/should-build/match"
	"gopkg.in/yaml.v3"
)

// Config is the top-level should-build configuration.
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

// Target defines a single build target.
type Target struct {
	Path    string   `yaml:"path"`
	Lang    string   `yaml:"lang"`
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// Load reads and validates a config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return Parse(data)
}

// Parse parses and validates config from raw YAML bytes.
func Parse(data []byte) (*Config, error) {
	var raw Config
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return Canonicalize(raw)
}

// Canonicalize validates cfg and returns a new Config with defaults applied.
//
// Defaults:
//   - unknown_file defaults to "trigger_all" when empty.
//   - lang defaults to "go" when path is set, "none" otherwise.
//
// All glob patterns are validated. An error is returned for any invalid
// field value or malformed pattern.
func Canonicalize(cfg Config) (*Config, error) {
	switch cfg.UnknownFile {
	case "":
		cfg.UnknownFile = "trigger_all"
	case "trigger_all", "ignore":
		// valid
	default:
		return nil, fmt.Errorf("unknown_file: must be %q or %q, got %q", "trigger_all", "ignore", cfg.UnknownFile)
	}

	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("config must define at least one target")
	}

	if err := validatePatterns(cfg.Global.Ignore); err != nil {
		return nil, fmt.Errorf("global.ignore: %w", err)
	}
	if err := validatePatterns(cfg.Global.TriggerAll); err != nil {
		return nil, fmt.Errorf("global.trigger_all: %w", err)
	}

	targets := make(map[string]Target, len(cfg.Targets))
	for name, t := range cfg.Targets {
		ct, err := canonicalizeTarget(name, t)
		if err != nil {
			return nil, err
		}
		targets[name] = ct
	}
	cfg.Targets = targets
	return &cfg, nil
}

func canonicalizeTarget(name string, t Target) (Target, error) {
	switch t.Lang {
	case "", "go", "none":
		// valid; apply defaults below
	default:
		return Target{}, fmt.Errorf("target %q: lang must be %q or %q, got %q", name, "go", "none", t.Lang)
	}

	// Default lang: "go" when path is set, "none" otherwise.
	if t.Lang == "" {
		if t.Path != "" {
			t.Lang = "go"
		} else {
			t.Lang = "none"
		}
	}

	for _, p := range t.Include {
		expanded := match.ExpandTarget(p, name)
		if err := match.ValidatePattern(expanded); err != nil {
			return Target{}, fmt.Errorf("target %q include: %w", name, err)
		}
	}
	for _, p := range t.Exclude {
		expanded := match.ExpandTarget(p, name)
		if err := match.ValidatePattern(expanded); err != nil {
			return Target{}, fmt.Errorf("target %q exclude: %w", name, err)
		}
	}
	return t, nil
}

func validatePatterns(patterns []string) error {
	for _, p := range patterns {
		if err := match.ValidatePattern(p); err != nil {
			return err
		}
	}
	return nil
}
