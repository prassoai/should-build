// Command should-build decides which targets need rebuilding based on
// dependency-graph analysis of changes between two git refs.
//
// Usage:
//
//	should-build [flags] <base-ref> <head-ref>
//
// See the project README for the full config schema and CLI reference.
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
	os.Exit(mainExitCode())
}

// stringSlice is a flag.Value that collects repeated --target flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func mainExitCode() int {
	var (
		cfgPath  string
		targets  stringSlice
		useJSON  bool
		quiet    bool
		verbose  bool
		repoPath string
	)
	flag.StringVar(&cfgPath, "config", "", "path to config file (default: <repo>/should-build.yaml)")
	flag.Var(&targets, "target", "evaluate only this target (repeatable)")
	flag.BoolVar(&useJSON, "json", false, "output JSON")
	flag.BoolVar(&quiet, "quiet", false, "exit-code only: 0=skip, 1=rebuild")
	flag.BoolVar(&verbose, "verbose", false, "show per-file match details (requires --json)")
	flag.StringVar(&repoPath, "repo", ".", "repository root")
	flag.Parse()

	if flag.NArg() != 2 {
		fmt.Fprintf(os.Stderr, "usage: should-build [flags] <base-ref> <head-ref>\n")
		return 2
	}
	if verbose && !useJSON {
		fmt.Fprintf(os.Stderr, "should-build: --verbose requires --json\n")
		return 2
	}

	// Default config path to <repo>/should-build.yaml when not explicitly set.
	if cfgPath == "" {
		cfgPath = filepath.Join(repoPath, "should-build.yaml")
	}

	results, err := evaluate(cfgPath, repoPath, flag.Arg(0), flag.Arg(1), targets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "should-build: %v\n", err)
		return 2
	}

	if quiet {
		for _, r := range results {
			if r.Build {
				return 1
			}
		}
		return 0
	}

	var printErr error
	if useJSON {
		printErr = printJSON(os.Stdout, results, verbose)
	} else {
		printErr = printTable(os.Stdout, results)
	}
	if printErr != nil {
		fmt.Fprintf(os.Stderr, "should-build: %v\n", printErr)
		return 2
	}
	return 0
}

func evaluate(cfgPath, repoPath, base, head string, filterTargets []string) ([]eval.Result, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	if len(filterTargets) > 0 {
		filtered := make(map[string]config.Target, len(filterTargets))
		var unknown []string
		for _, name := range filterTargets {
			t, ok := cfg.Targets[name]
			if !ok {
				unknown = append(unknown, name)
				continue
			}
			filtered[name] = t
		}
		if len(unknown) > 0 {
			return nil, fmt.Errorf("unknown targets: %s", strings.Join(unknown, ", "))
		}
		cfg.Targets = filtered
	}

	changedFiles, err := diff.Changed(repoPath, base, head)
	if err != nil {
		return nil, err
	}

	deps := make(map[string][]string)
	var analyzer depgraph.Go
	for name, t := range cfg.Targets {
		if t.ResolvedLang() != "go" {
			continue
		}
		d, err := analyzer.Deps(repoPath, t.Path)
		if err != nil {
			return nil, fmt.Errorf("target %s: %w", name, err)
		}
		deps[name] = d
	}

	return eval.Evaluate(cfg, changedFiles, deps), nil
}

// printJSON marshals results as JSON. In non-verbose mode, the Rule field
// is zeroed so omitempty drops it.
func printJSON(w io.Writer, results []eval.Result, verbose bool) error {
	if !verbose {
		for i := range results {
			for j := range results[i].Files {
				results[i].Files[j].Rule = ""
			}
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func printTable(w io.Writer, results []eval.Result) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tBUILD\tREASON")
	for _, r := range results {
		build := "no"
		reason := "-"
		if r.Build {
			build = "yes"
			reason = r.Files[0].Path + " (" + r.Files[0].Reason + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Target, build, reason)
	}
	return tw.Flush()
}
