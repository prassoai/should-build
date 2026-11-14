// Package depgraph computes the set of files transitively imported by a
// Go build target.
//
// The Go analyzer uses "go list -json -deps" to resolve the full transitive
// dependency graph, then returns file paths relative to the repo root. It
// collects GoFiles, CgoFiles, CFiles, CXXFiles, FFiles, MFiles, HFiles, SFiles,
// SysoFiles, SwigFiles, SwigCXXFiles, and EmbedFiles, following Go's package
// build-input lists. This includes non-cgo assembly and system object files.
// File selection follows the current Go environment and build tags, without
// collecting ignored files, test-only inputs, or arbitrary header includes.
// Go.DepsAll resolves multiple package patterns in one invocation while keeping
// each pattern's dependency closure separate; Go.Deps resolves one pattern.
//
// Files outside the repo root (standard library packages, vendored modules
// fetched to GOMODCACHE, replace-directive targets above the repo) are
// silently excluded from the returned set. Only the current source tree is
// analyzed; deleted inputs and dependencies not returned in these go list
// fields (such as external headers and libraries) are not tracked.
package depgraph
