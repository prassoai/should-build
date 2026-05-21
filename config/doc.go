// Package config defines the should-build configuration schema and loader.
//
// A Config describes which files trigger rebuilds of which targets.
// It is loaded from a YAML file (typically should-build.yaml at the repo root).
//
// The schema has three layers:
//   - Global rules (ignore, trigger_all) that apply across all targets.
//   - Per-target rules (include, exclude) with doublestar globs and {target} expansion.
//   - Per-target triggers that propagate builds to other targets.
//   - An unknown_file fallback policy for files matching no rule.
//
// # Target triggers
//
// A target may declare a triggers list naming other targets that must also
// build whenever it builds. Triggers propagate transitively: if A triggers B
// and B triggers C, building A also builds B and C. Cycles are rejected at
// parse time — they are a configuration error.
//
// # Defaults applied by Canonicalize
//
// unknown_file defaults to "trigger_all" when the field is empty or absent.
// This is the safe default: unrecognized files rebuild everything.
//
// lang defaults to "go" when the target has a path set, and "none" otherwise.
// A target with lang "go" runs the Go dependency-graph analyzer. A target
// with lang "none" relies solely on include/exclude patterns.
//
// Load reads and validates a file. Parse does the same from raw bytes.
// Canonicalize applies defaults and validates a pre-built Config struct.
// All three functions validate every glob pattern at call time so that
// pattern matching at evaluation time cannot fail.
package config
