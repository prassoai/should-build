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
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validate(cfg *Config) error {
	// Validate and default unknown_file.
	switch cfg.UnknownFile {
	case "":
		cfg.UnknownFile = "trigger_all"
	case "trigger_all", "ignore":
		// valid
	default:
		return fmt.Errorf("unknown_file: must be %q or %q, got %q", "trigger_all", "ignore", cfg.UnknownFile)
	}

	if len(cfg.Targets) == 0 {
		return fmt.Errorf("config must define at least one target")
	}

	// Validate global patterns.
	if err := validatePatterns(cfg.Global.Ignore); err != nil {
		return fmt.Errorf("global.ignore: %w", err)
	}
	if err := validatePatterns(cfg.Global.TriggerAll); err != nil {
		return fmt.Errorf("global.trigger_all: %w", err)
	}

	// Validate and default each target.
	for name, t := range cfg.Targets {
		if err := validateTarget(name, &t); err != nil {
			return err
		}
		cfg.Targets[name] = t
	}
	return nil
}

func validateTarget(name string, t *Target) error {
	switch t.Lang {
	case "", "go", "none":
		// valid; apply defaults below
	default:
		return fmt.Errorf("target %q: lang must be %q or %q, got %q", name, "go", "none", t.Lang)
	}

	// Default lang based on path presence.
	if t.Lang == "" {
		if t.Path != "" {
			t.Lang = "go"
		} else {
			t.Lang = "none"
		}
	}

	// Expand {target} then validate patterns.
	for _, p := range t.Include {
		expanded := match.ExpandTarget(p, name)
		if err := match.ValidatePattern(expanded); err != nil {
			return fmt.Errorf("target %q include: %w", name, err)
		}
	}
	for _, p := range t.Exclude {
		expanded := match.ExpandTarget(p, name)
		if err := match.ValidatePattern(expanded); err != nil {
			return fmt.Errorf("target %q exclude: %w", name, err)
		}
	}
	return nil
}

func validatePatterns(patterns []string) error {
	for _, p := range patterns {
		if err := match.ValidatePattern(p); err != nil {
			return err
		}
	}
	return nil
}
