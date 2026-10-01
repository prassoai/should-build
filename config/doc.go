// Package config defines the should-build configuration schema and loader.
//
// A Config describes which files trigger rebuilds of which targets.
// It is loaded from a YAML file (typically should-build.yaml at the repo root).
//
// The schema has five layers:
//   - Global rules (ignore, trigger_all) that apply across all targets.
//   - Per-target rules (include, exclude) with doublestar globs and {target} expansion.
//   - Per-target triggers that propagate builds to other targets.
//   - An unknown_file fallback policy for files matching no rule.
//   - A per-target selection mode deciding whether the repo-wide rules
//     (trigger_all, unknown_file) may select that target at all.
//
// # Selection modes
//
// should-build defaults to failing open: global.trigger_all and the
// unknown_file fallback reach every target, so an unrecognized change
// rebuilds everything. That is the right trade for a build measured in
// minutes, and the wrong one for a target measured in machine-hours — a VM
// image bake selected by an orphaned test fixture is pure waste.
//
// A target sets selection: explicit to opt out of exactly those two
// repo-wide rules. It is then selected only by rules that name its own
// inputs: its include patterns, its dependency graph, and triggers declared
// by other targets. global.ignore, exclude, include, the dependency graph and
// trigger propagation are unchanged, and other targets keep their fail-open
// behavior — the mode is per-target, with no global switch.
//
// An explicit target still claims its include patterns for orphan
// classification, so declaring the inputs of an expensive target also stops
// those files reaching the unknown_file fallback of every other target.
//
// Because the mode's failure direction is a target that silently never
// builds, Canonicalize rejects an explicit target that nothing can select:
// no include patterns, no Go dependency graph, and no incoming trigger.
//
// # Target triggers
//
// A target may declare a triggers list naming other targets that must also
// build whenever it builds. Triggers propagate transitively: if A triggers B
// and B triggers C, building A also builds B and C. Cycles are rejected at
// parse time — they are a configuration error.
//
// The field is named "triggers" (on the source target) rather than "depends_on"
// (on the dependent target) because the relationship is declared at the source:
// "building murmur-control triggers murmur-vm." This keeps the co-build
// relationship in one place rather than scattering it across dependent targets.
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
// selection defaults to "fail_open", preserving the behavior of configs
// written before the field existed.
//
// Load reads and validates a file. Parse does the same from raw bytes.
// Canonicalize applies defaults and validates a pre-built Config struct.
// All three functions validate every glob pattern at call time so that
// pattern matching at evaluation time cannot fail.
package config
