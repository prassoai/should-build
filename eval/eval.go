package eval

import (
	"sort"

	"github.com/prassoai/should-build/config"
	"github.com/prassoai/should-build/match"
)

// Result describes whether a target needs rebuilding and why.
type Result struct {
	Target string      `json:"target"`
	Build  bool        `json:"build"`
	Files  []FileMatch `json:"files"`
}

// FileMatch records why a changed file triggered a target.
type FileMatch struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`           // "go-dep", "include", "trigger-all", "unknown-file"
	Rule   string `json:"rule,omitempty"`   // the specific glob or import path
}

// Evaluate determines which targets need rebuilding.
//
// changed is the list of file paths (relative to repo root) modified between
// the base and head commits. deps maps target names to the set of file paths
// each target transitively depends on (from the language-specific analyzer).
func Evaluate(cfg *config.Config, changed []string, deps map[string][]string) []Result {
	// Pre-filter globally ignored files.
	filtered := make([]string, 0, len(changed))
	for _, f := range changed {
		if ignored, _, _ := match.MatchAny(cfg.Global.Ignore, f); !ignored {
			filtered = append(filtered, f)
		}
	}

	// Index dep sets for O(1) lookup per file.
	depSets := make(map[string]map[string]struct{}, len(deps))
	for target, files := range deps {
		s := make(map[string]struct{}, len(files))
		for _, f := range files {
			s[f] = struct{}{}
		}
		depSets[target] = s
	}

	// Sort target names for deterministic output.
	names := make([]string, 0, len(cfg.Targets))
	for name := range cfg.Targets {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make([]Result, 0, len(names))
	for _, name := range names {
		target := cfg.Targets[name]
		results = append(results, evaluateTarget(cfg, name, &target, filtered, depSets[name]))
	}
	return results
}

func evaluateTarget(cfg *config.Config, name string, target *config.Target, files []string, depSet map[string]struct{}) Result {
	r := Result{
		Target: name,
		Files:  []FileMatch{}, // never nil — JSON encodes as []
	}

	includes := expandPatterns(target.Include, name)
	excludes := expandPatterns(target.Exclude, name)

	for _, f := range files {
		// Precedence step 2: target exclude.
		if excluded, _, _ := match.MatchAny(excludes, f); excluded {
			continue
		}

		// Precedence step 3: target include.
		if ok, rule, _ := match.MatchAny(includes, f); ok {
			r.Build = true
			r.Files = append(r.Files, FileMatch{Path: f, Reason: "include", Rule: rule})
			continue
		}

		// Precedence step 4: dependency graph.
		if depSet != nil {
			if _, ok := depSet[f]; ok {
				r.Build = true
				r.Files = append(r.Files, FileMatch{Path: f, Reason: "go-dep"})
				continue
			}
		}

		// Precedence step 5: global trigger_all.
		if ok, rule, _ := match.MatchAny(cfg.Global.TriggerAll, f); ok {
			r.Build = true
			r.Files = append(r.Files, FileMatch{Path: f, Reason: "trigger-all", Rule: rule})
			continue
		}

		// Precedence step 6: unknown file fallback.
		if cfg.UnknownFile == "trigger_all" {
			r.Build = true
			r.Files = append(r.Files, FileMatch{Path: f, Reason: "unknown-file"})
		}
	}
	return r
}

func expandPatterns(patterns []string, target string) []string {
	out := make([]string, len(patterns))
	for i, p := range patterns {
		out[i] = match.ExpandTarget(p, target)
	}
	return out
}
