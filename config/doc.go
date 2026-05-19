// Package config defines the should-build configuration schema and loader.
//
// A Config describes which files trigger rebuilds of which targets.
// It is loaded from a YAML file (typically should-build.yaml at the repo root).
//
// The schema has three layers:
//   - Global rules (ignore, trigger_all) that apply across all targets.
//   - Per-target rules (include, exclude) with {target} template expansion.
//   - An unknown_file fallback policy for files matching no rule.
//
// Load reads and validates a file. Parse does the same from raw bytes.
// Both apply defaults (unknown_file defaults to "trigger_all", lang defaults
// based on whether path is set) and validate all glob patterns.
package config
