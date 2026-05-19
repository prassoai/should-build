package depgraph

// Analyzer computes the set of files transitively imported by a target.
// Implementations are language-specific.
type Analyzer interface {
	// Deps returns file paths (relative to repoRoot) that the target at
	// importPath transitively depends on, including the target's own files.
	Deps(repoRoot, importPath string) ([]string, error)
}
