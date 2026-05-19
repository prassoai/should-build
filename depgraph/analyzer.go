package depgraph

// Analyzer computes the set of source files transitively imported by a
// target. Implementations are language-specific.
type Analyzer interface {
	// Deps returns file paths (relative to repoRoot) that the target at
	// importPath transitively depends on, including the target's own
	// source files.
	Deps(repoRoot, importPath string) ([]string, error)
}
