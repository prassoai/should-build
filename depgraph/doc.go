// Package depgraph defines the interface for language-specific dependency analysis
// and provides a Go implementation.
//
// An Analyzer computes the set of files transitively imported by a build target.
// The Go analyzer uses "go list -json -deps" to resolve the full transitive
// dependency graph, then returns file paths relative to the repo root.
//
// Files outside the repo root (standard library packages, vendored modules
// fetched to GOMODCACHE, replace-directive targets above the repo) are
// silently excluded from the returned set. This is intentional: such files
// cannot appear in a git diff and therefore cannot trigger rebuilds.
//
// Adding a new language means implementing Analyzer and registering it under
// a new lang value in the config. The interface is intentionally narrow:
// one method, one return type.
package depgraph
