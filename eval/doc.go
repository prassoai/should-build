// Package eval determines which build targets need rebuilding.
//
// Evaluate is a pure function: it takes a parsed config, a list of changed
// file paths, and precomputed dependency graphs, and returns a decision for
// each target. It has no filesystem, git, or network dependencies.
//
// Evaluation precedence for each changed file against each target:
//  1. global.ignore — file is invisible to all targets.
//  2. target.exclude — target opts out of this file.
//  3. target.include — file triggers target.
//  4. Dependency graph — file is in a package the target imports.
//  5. global.trigger_all — file triggers all non-excluded targets.
//  6. unknown_file policy — fallback for files matching no rule.
//
// After per-target evaluation, trigger propagation activates additional
// targets: if target A builds and declares triggers: [B], then B is also
// marked as building with reason "triggered-by". Propagation is transitive
// (A→B→C) and the config layer guarantees no cycles. Targets already
// building from their own rules are not given a redundant triggered-by entry.
//
// When a target's include or exclude pattern uses {target}, the Rule field
// in FileMatch stores the expanded pattern (e.g. "targets/api/conf/*.yaml"),
// not the original template. This aids debugging by showing exactly what
// matched without requiring the reader to mentally substitute the target name.
package eval
