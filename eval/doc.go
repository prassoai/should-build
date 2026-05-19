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
package eval
