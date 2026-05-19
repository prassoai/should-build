package config

import (
	"bytes"
	"fmt"
	"os"

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
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.UnknownFile == "" {
		cfg.UnknownFile = "trigger_all"
	}
	return &cfg, validate(&cfg)
}

func validate(cfg *Config) error {
	switch cfg.UnknownFile {
	case "trigger_all", "ignore":
	default:
		return fmt.Errorf("unknown_file: invalid value %q (want %q or %q)", cfg.UnknownFile, "trigger_all", "ignore")
	}
	if len(cfg.Targets) == 0 {
		return fmt.Errorf("config must define at least one target")
	}
	for name, t := range cfg.Targets {
		switch lang := t.ResolvedLang(); lang {
		case "go":
			if t.Path == "" {
				return fmt.Errorf("target %q: lang %q requires path", name, lang)
			}
		case "none":
		default:
			return fmt.Errorf("target %q: unsupported lang %q", name, t.Lang)
		}
	}
	return nil
}
