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
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`         // "go-dep", "include", "trigger-all", "triggered-by", "unknown-file"
	Rule   string `json:"rule,omitempty"` // the specific glob, import path, or trigger source target
}

// Evaluate determines which targets need rebuilding.
//
// changed is the list of file paths (relative to repo root) modified between
// the base and head commits. deps maps target names to the set of file paths
// each target transitively depends on (from the language-specific analyzer).
//
// only restricts which targets are initially evaluated against the diff.
// When empty, all targets in cfg are evaluated. Trigger propagation may
// expand the result set beyond the initial set: if a building target triggers
// another target not in only, that target is evaluated and added to the output.
func Evaluate(cfg *config.Config, changed []string, deps map[string][]string, only []string) []Result {
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

	// Determine initial evaluation set.
	var names []string
	if len(only) > 0 {
		names = make([]string, len(only))
		copy(names, only)
	} else {
		names = make([]string, 0, len(cfg.Targets))
		for name := range cfg.Targets {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	results := make([]Result, 0, len(names))
	for _, name := range names {
		results = append(results, evaluateTarget(cfg, name, cfg.Targets[name], filtered, depSets[name]))
	}

	return propagateTriggers(cfg, filtered, depSets, results)
}

func evaluateTarget(cfg *config.Config, name string, target config.Target, files []string, depSet map[string]struct{}) Result {
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

// propagateTriggers applies the trigger graph: if target A builds and
// triggers B, B is also marked as building. When B is not yet in the result
// set (e.g. excluded by --target), it is fully evaluated against the diff
// and added. All trigger sources are recorded when multiple targets trigger
// the same dependent. Targets already building from their own rules do not
// get a redundant triggered-by entry.
//
// Returns a new slice sorted by target name. The input slice is not modified.
func propagateTriggers(cfg *config.Config, files []string, depSets map[string]map[string]struct{}, initial []Result) []Result {
	results := make([]Result, len(initial))
	copy(results, initial)

	idx := make(map[string]int, len(results))
	for i, r := range results {
		idx[r.Target] = i
	}

	for changed := true; changed; {
		changed = false
		n := len(results)
		for i := 0; i < n; i++ {
			if !results[i].Build {
				continue
			}
			for _, triggered := range cfg.Targets[results[i].Target].Triggers {
				j, inSet := idx[triggered]
				if !inSet {
					// Triggered target not yet evaluated — evaluate and add.
					t := cfg.Targets[triggered]
					r := evaluateTarget(cfg, triggered, t, files, depSets[triggered])
					if !hasOwnBuildReason(r) {
						r.Build = true
						r.Files = append(r.Files, FileMatch{
							Reason: "triggered-by",
							Rule:   results[i].Target,
						})
					}
					idx[triggered] = len(results)
					results = append(results, r)
					changed = true
					continue
				}
				// Target already in result set.
				if hasOwnBuildReason(results[j]) {
					continue // already building from own rules — no triggered-by needed
				}
				if !results[j].Build {
					results[j].Build = true
					results[j].Files = append(results[j].Files, FileMatch{
						Reason: "triggered-by",
						Rule:   results[i].Target,
					})
					changed = true
					continue
				}
				// Already building from triggers — record additional source if new.
				if !hasTriggeredBy(results[j], results[i].Target) {
					results[j].Files = append(results[j].Files, FileMatch{
						Reason: "triggered-by",
						Rule:   results[i].Target,
					})
				}
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Target < results[j].Target
	})
	return results
}

// hasOwnBuildReason reports whether r builds from its own rules (not just triggers).
func hasOwnBuildReason(r Result) bool {
	for _, fm := range r.Files {
		if fm.Reason != "triggered-by" {
			return true
		}
	}
	return false
}

// hasTriggeredBy reports whether r already has a triggered-by entry from source.
func hasTriggeredBy(r Result, source string) bool {
	for _, fm := range r.Files {
		if fm.Reason == "triggered-by" && fm.Rule == source {
			return true
		}
	}
	return false
}

func expandPatterns(patterns []string, target string) []string {
	out := make([]string, len(patterns))
	for i, p := range patterns {
		out[i] = match.ExpandTarget(p, target)
	}
	return out
}
