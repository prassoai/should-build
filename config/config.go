package config

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

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
	Path     string   `yaml:"path"`
	Lang     string   `yaml:"lang"`
	Include  []string `yaml:"include"`
	Exclude  []string `yaml:"exclude"`
	Triggers []string `yaml:"triggers"`
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

	if err := validateTriggers(cfg.Targets); err != nil {
		return nil, err
	}
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

// validateTriggers checks that all trigger references point to existing targets
// and that the trigger graph is acyclic.
func validateTriggers(targets map[string]Target) error {
	for name, t := range targets {
		for _, ref := range t.Triggers {
			if _, ok := targets[ref]; !ok {
				return fmt.Errorf("target %q triggers unknown target %q", name, ref)
			}
			if ref == name {
				return fmt.Errorf("target %q triggers itself", name)
			}
		}
	}
	return detectCycle(targets)
}

// detectCycle uses DFS to find cycles in the trigger graph.
// The outer loop iterates targets in sorted order for deterministic error messages.
func detectCycle(targets map[string]Target) error {
	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS path
		black = 2 // fully explored
	)
	color := make(map[string]int, len(targets))
	parent := make(map[string]string, len(targets))

	var visit func(string) error
	visit = func(name string) error {
		color[name] = gray
		for _, ref := range targets[name].Triggers {
			switch color[ref] {
			case gray:
				cycle := []string{ref, name}
				for cur := name; parent[cur] != "" && cur != ref; cur = parent[cur] {
					cycle = append(cycle, parent[cur])
				}
				slices.Reverse(cycle)
				return fmt.Errorf("trigger cycle: %s", strings.Join(cycle, " -> "))
			case white:
				parent[ref] = name
				if err := visit(ref); err != nil {
					return err
				}
			}
		}
		color[name] = black
		return nil
	}

	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if color[name] == white {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}
