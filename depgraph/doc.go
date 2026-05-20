// Package depgraph computes the set of files transitively imported by a
// Go build target.
//
// The Go analyzer uses "go list -json -deps" to resolve the full transitive
// dependency graph, then returns file paths relative to the repo root.
//
// Files outside the repo root (standard library packages, vendored modules
// fetched to GOMODCACHE, replace-directive targets above the repo) are
// silently excluded from the returned set. This is intentional: such files
// cannot appear in a git diff and therefore cannot trigger rebuilds.
package depgraph
