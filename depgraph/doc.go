// Package depgraph computes the set of files transitively imported by a
// Go build target.
//
// The Go analyzer uses "go list -json -deps" to resolve the full transitive
// dependency graph, then returns file paths relative to the repo root. Both
// Go source files (GoFiles) and //go:embed assets (EmbedFiles) are returned,
// so a change to an embedded file rebuilds the binaries that embed it.
// Go.DepsAll resolves multiple package patterns in one invocation while keeping
// each pattern's dependency closure separate.
//
// Go.DepsAt does the same for an earlier revision, checked out from git into a
// temporary directory. A file deleted by a change exists only there, so the
// caller unions both graphs: the working tree attributes added and modified
// files, the earlier revision attributes deleted ones. Loading at a revision
// is best effort (go list -e), since a target added by the change has no
// earlier graph to find. The checkout keeps the working tree's Go workspace:
// a go.work inside the repository, tracked or not, is mapped to the same path
// in the checkout, and one outside the repository is turned off.
//
// Files outside the repo root (standard library packages, vendored modules
// fetched to GOMODCACHE, replace-directive targets above the repo) are
// silently excluded from the returned set. This is intentional: such files
// cannot appear in a git diff and therefore cannot trigger rebuilds.
package depgraph
