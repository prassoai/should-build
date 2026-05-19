// Package diff computes the set of files changed between two git commits.
//
// Changed shells out to git diff --name-only, which handles renames
// (reported as delete + add), binary files, and submodule pointer changes.
// Paths are always forward-slash-separated and relative to the repo root.
package diff
