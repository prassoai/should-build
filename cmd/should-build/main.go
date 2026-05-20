// Command should-build determines which build targets need rebuilding
// after a code change. It combines a declarative config file with
// language-specific dependency-graph analysis and git diff to answer
// a single question per target: rebuild, or skip.
//
// Usage:
//
//	should-build [flags] <base-ref> <head-ref>
//
// Flags:
//
//	--config <path>    Path to config file (default: should-build.yaml, relative to --repo)
//	--target <name>    Evaluate only this target (repeatable)
//	--json             Output JSON
//	--quiet            Exit 0 if nothing to rebuild, 1 if any target needs rebuilding
//	--verbose          Show per-file match rules in output
//	--explain          Write human-readable explanation to stderr
//	--repo <path>      Repository root (default: current directory)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/prassoai/should-build/config"
	"github.com/prassoai/should-build/depgraph"
	"github.com/prassoai/should-build/diff"
	"github.com/prassoai/should-build/eval"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("should-build", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		configPath = fs.String("config", "should-build.yaml", "path to config file")
		jsonOut    = fs.Bool("json", false, "output JSON")
		quiet      = fs.Bool("quiet", false, "exit-code only: 0 = no rebuild, 1 = rebuild needed")
		verbose    = fs.Bool("verbose", false, "show per-file match rules in output")
		explain    = fs.Bool("explain", false, "write human-readable explanation to stderr")
		repoPath   = fs.String("repo", ".", "repository root")
		targets    stringSlice
	)
	fs.Var(&targets, "target", "evaluate only this target (repeatable)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintf(stderr, "usage: should-build [flags] <base-ref> <head-ref>\n")
		return 2
	}
	baseRef, headRef := fs.Arg(0), fs.Arg(1)

	if *quiet && *jsonOut {
		fmt.Fprintf(stderr, "error: --quiet and --json are mutually exclusive\n")
		return 2
	}

	// Resolve config path relative to repo root.
	cfgFile := *configPath
	if !filepath.IsAbs(cfgFile) {
		cfgFile = filepath.Join(*repoPath, cfgFile)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	// Filter to requested targets without mutating the loaded config.
	if len(targets) > 0 {
		filtered := make(map[string]config.Target, len(targets))
		for _, name := range targets {
			t, ok := cfg.Targets[name]
			if !ok {
				fmt.Fprintf(stderr, "error: unknown target %q\n", name)
				return 2
			}
			filtered[name] = t
		}
		cfg = &config.Config{
			Global:      cfg.Global,
			UnknownFile: cfg.UnknownFile,
			Targets:     filtered,
		}
	}

	changed, err := diff.Changed(*repoPath, baseRef, headRef)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	// Compute dependency graphs for Go targets.
	// Failure is a hard error — silently skipping hides misconfiguration.
	var analyzer depgraph.Go
	deps := make(map[string][]string)
	for name, t := range cfg.Targets {
		if t.Lang != "go" || t.Path == "" {
			continue
		}
		d, err := analyzer.Deps(*repoPath, t.Path)
		if err != nil {
			fmt.Fprintf(stderr, "error: dep graph for %s: %v\n", name, err)
			return 2
		}
		deps[name] = d
	}

	results := eval.Evaluate(cfg, changed, deps)

	// --explain is orthogonal to the output mode: it always writes to stderr.
	if *explain {
		writeExplain(stderr, results)
	}

	if *quiet {
		for _, r := range results {
			if r.Build {
				return 1
			}
		}
		return 0
	}

	if *jsonOut {
		return writeJSON(stdout, stderr, results, *verbose)
	}
	return writeTable(stdout, results, *verbose)
}

func writeJSON(stdout, stderr io.Writer, results []eval.Result, verbose bool) int {
	out := results
	if !verbose {
		out = stripRules(results)
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	return 0
}

// stripRules returns a copy of results with Rule fields cleared.
func stripRules(results []eval.Result) []eval.Result {
	out := make([]eval.Result, len(results))
	for i, r := range results {
		files := make([]eval.FileMatch, len(r.Files))
		for j, fm := range r.Files {
			files[j] = eval.FileMatch{Path: fm.Path, Reason: fm.Reason}
		}
		out[i] = eval.Result{Target: r.Target, Build: r.Build, Files: files}
	}
	return out
}

// writeExplain writes a human-readable explanation of the evaluation to w.
// Called when --explain is set so CI logs show why each target was or wasn't
// rebuilt without requiring humans to parse JSON.
func writeExplain(w io.Writer, results []eval.Result) {
	var body strings.Builder
	rebuilds := 0
	for _, r := range results {
		if r.Build {
			rebuilds++
		}
		body.WriteString(formatExplainResult(r))
		body.WriteByte('\n')
	}
	fmt.Fprintf(w, "should-build: %d targets evaluated, %d rebuilding\n\n%s\n", len(results), rebuilds, body.String())
}

// formatExplainResult formats a single evaluation result as a human-readable
// block. Pure function — no I/O, easy to unit-test.
func formatExplainResult(r eval.Result) string {
	if !r.Build {
		return "  " + r.Target + ": skip"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s: rebuild (%d files)", r.Target, len(r.Files))
	for _, fm := range r.Files {
		b.WriteString("\n    ")
		b.WriteString(fm.Path)
		b.WriteString("  (")
		b.WriteString(fm.Reason)
		if fm.Rule != "" {
			b.WriteString(": ")
			b.WriteString(fm.Rule)
		}
		b.WriteByte(')')
	}
	return b.String()
}

func writeTable(w io.Writer, results []eval.Result, verbose bool) int {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "TARGET\tBUILD\tREASON\n")
	for _, r := range results {
		if !r.Build {
			fmt.Fprintf(tw, "%s\tno\t-\n", r.Target)
			continue
		}
		if verbose {
			for _, fm := range r.Files {
				fmt.Fprintf(tw, "%s\tyes\t%s (%s: %s)\n", r.Target, fm.Path, fm.Reason, fm.Rule)
			}
		} else {
			fm := r.Files[0]
			fmt.Fprintf(tw, "%s\tyes\t%s (%s)\n", r.Target, fm.Path, fm.Reason)
		}
	}
	tw.Flush()
	return 0
}

type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprintf("%v", *s) }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
