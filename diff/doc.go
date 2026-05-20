// Package diff computes the set of files changed between two git commits.
//
// Rename detection is disabled via --no-renames so each path appears
// verbatim as an add or delete. Binary files and submodule pointer
// changes are included. Paths are always forward-slash-separated and
// relative to the repo root.
package diff
