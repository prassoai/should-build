package eval

import (
	"sort"

	"github.com/prassoai/should-build/config"
	"github.com/prassoai/should-build/match"
)

// FileMatch records why a changed file triggered a target.
type FileMatch struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`         // "include", "go-dep", "trigger-all", "unknown-file"
	Rule   string `json:"rule,omitempty"` // specific glob or file that matched (verbose)
}

// Result is the rebuild decision for one target.
type Result struct {
	Target string      `json:"target"`
	Build  bool        `json:"build"`
	Files  []FileMatch `json:"files"`
}

// Evaluate returns a rebuild decision for every target in cfg.
// changedFiles are paths relative to repo root (forward-slash separated).
// deps maps target name → file paths the target transitively depends on.
func Evaluate(cfg *config.Config, changedFiles []string, deps map[string][]string) []Result {
	// Pre-filter globally ignored files.
	relevant := filterIgnored(changedFiles, cfg.Global.Ignore)

	// Build dep-set lookups for O(1) membership checks.
	depSets := make(map[string]map[string]bool, len(deps))
	for name, files := range deps {
		s := make(map[string]bool, len(files))
		for _, f := range files {
			s[f] = true
		}
		depSets[name] = s
	}

	names := sortedKeys(cfg.Targets)
	results := make([]Result, 0, len(names))
	for _, name := range names {
		t := cfg.Targets[name]
		include := match.ExpandAll(t.Include, name)
		exclude := match.ExpandAll(t.Exclude, name)

		files := []FileMatch{} // non-nil for JSON "files": []
		for _, f := range relevant {
			if m := classifyFile(f, include, exclude, depSets[name], t, cfg); m.Reason != "" {
				files = append(files, m)
			}
		}
		results = append(results, Result{
			Target: name,
			Build:  len(files) > 0,
			Files:  files,
		})
	}
	return results
}

// classifyFile applies the precedence rules for one file against one target.
// Returns a zero FileMatch if the file does not trigger the target.
func classifyFile(
	file string,
	include, exclude []string,
	deps map[string]bool,
	t config.Target,
	cfg *config.Config,
) FileMatch {
	// Precedence 2: target.exclude
	if _, ok := match.Any(file, exclude); ok {
		return FileMatch{}
	}
	// Precedence 3: target.include
	if pat, ok := match.Any(file, include); ok {
		return FileMatch{Path: file, Reason: "include", Rule: pat}
	}
	// Precedence 4: dep graph
	if t.ResolvedLang() != "none" && deps[file] {
		return FileMatch{Path: file, Reason: "go-dep", Rule: file}
	}
	// Precedence 5: global.trigger_all
	if pat, ok := match.Any(file, cfg.Global.TriggerAll); ok {
		return FileMatch{Path: file, Reason: "trigger-all", Rule: pat}
	}
	// Precedence 6: unknown_file policy
	if cfg.UnknownFile == "trigger_all" {
		return FileMatch{Path: file, Reason: "unknown-file", Rule: "unknown-file-policy"}
	}
	return FileMatch{}
}

func filterIgnored(files []string, patterns []string) []string {
	if len(patterns) == 0 {
		return files
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		if _, ok := match.Any(f, patterns); !ok {
			out = append(out, f)
		}
	}
	return out
}

func sortedKeys(m map[string]config.Target) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
