// Package eval implements the core should-build decision engine.
//
// It combines config rules, changed files, and dependency graphs into
// per-target rebuild decisions. The [Evaluate] function is pure — no I/O,
// no side effects — making it trivially testable with table-driven cases.
//
// Precedence for each changed file against each target:
//
//  1. global.ignore → file is invisible to all targets.
//  2. target.exclude → target opts out of this file.
//  3. target.include → trigger rebuild.
//  4. dep-graph → trigger if file is in the target's transitive deps.
//  5. global.trigger_all → trigger rebuild.
//  6. unknown_file policy → trigger_all or ignore.
package eval
