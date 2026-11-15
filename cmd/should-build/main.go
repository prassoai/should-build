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
//	--verbose          Show per-file match rules in JSON and table output
//	--explain          Write human-readable explanation to stderr
//	--repo <path>      Repository root (default: current directory)
//	--version          Print version and exit
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/prassoai/should-build/config"
	"github.com/prassoai/should-build/depgraph"
	"github.com/prassoai/should-build/diff"
	"github.com/prassoai/should-build/eval"
)

// version and commit are set by ldflags at build time.
// See .github/workflows/release.yml.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("should-build", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		configPath  = fs.String("config", "should-build.yaml", "path to config file")
		jsonOut     = fs.Bool("json", false, "output JSON")
		quiet       = fs.Bool("quiet", false, "exit-code only: 0 = no rebuild, 1 = rebuild needed")
		verbose     = fs.Bool("verbose", false, "show per-file match rules in JSON and table output")
		explain     = fs.Bool("explain", false, "write human-readable explanation to stderr")
		repoPath    = fs.String("repo", ".", "repository root")
		showVersion = fs.Bool("version", false, "print version and exit")
		targets     stringSlice
	)
	fs.Var(&targets, "target", "evaluate only this target (repeatable)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "should-build %s (%s)\n", version, commit)
		return 0
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

	// Validate --target names without filtering the config. The full config
	// is passed to Evaluate so that trigger propagation can expand the
	// evaluation set beyond the named targets.
	for _, name := range targets {
		if _, ok := cfg.Targets[name]; !ok {
			fmt.Fprintf(stderr, "error: unknown target %q\n", name)
			return 2
		}
	}

	changed, err := diff.Changed(*repoPath, baseRef, headRef)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	deps, err := goDeps(cfg, *repoPath, baseRef, changed)
	if err != nil {
		fmt.Fprintf(stderr, "error: dep graphs: %v\n", err)
		return 2
	}

	results, err := eval.Evaluate(cfg, changed, deps, []string(targets))
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

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
	return writeTable(stdout, stderr, results, *verbose)
}

// goDeps returns each Go target's dependency files: the union, over the
// target's GOOS list, of the working tree's graph and baseRef's graph. The
// working tree sees added and modified files; only the base still knows which
// targets used a file the change deletes, so it loads only when needsBase says
// it can matter. Targets sharing a GOOS share one load of each revision.
func goDeps(cfg *config.Config, repoRoot, baseRef string, changed []string) (map[string][]string, error) {
	platforms := make(map[string][]string)
	for _, target := range cfg.Targets {
		if !target.HasDepGraph() {
			continue
		}
		for _, goos := range platformsOf(target) {
			platforms[goos] = append(platforms[goos], target.Path)
		}
	}
	type loaded struct {
		goos  string
		graph map[string][]string
		err   error
	}
	results := make(chan loaded)
	for goos, paths := range platforms {
		go func() {
			graph, err := platformDeps(repoRoot, baseRef, goos, paths, changed)
			results <- loaded{goos, graph, err}
		}()
	}
	graphs := make(map[string]map[string][]string, len(platforms))
	var errs []error
	for range platforms {
		r := <-results
		graphs[r.goos] = r.graph
		errs = append(errs, r.err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	deps := make(map[string][]string)
	for name, target := range cfg.Targets {
		if !target.HasDepGraph() {
			continue
		}
		for _, goos := range platformsOf(target) {
			deps[name] = append(deps[name], graphs[goos][target.Path]...)
		}
	}
	return deps, nil
}

// platformDeps loads paths for one GOOS ("" for the analyzer's own) from the
// working tree and, when needsBase says it can matter, from baseRef, and unions each path's two graphs.
func platformDeps(repoRoot, baseRef, goos string, paths, changed []string) (map[string][]string, error) {
	analyzer := depgraph.Go{}
	if goos != "" {
		analyzer.Env = []string{"GOOS=" + goos}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	head, err := analyzer.DepsAll(repoRoot, paths)
	if err != nil {
		return nil, fmt.Errorf("loading %s working tree: %w", cmp.Or(goos, "default GOOS"), err)
	}
	ctx, err := analyzer.Context(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("loading %s build context: %w", cmp.Or(goos, "default GOOS"), err)
	}
	needed, err := needsBase(repoRoot, baseRef, ctx, changed)
	if err != nil {
		return nil, err
	}
	if !needed {
		return head, nil
	}
	base, err := analyzer.DepsAt(repoRoot, baseRef, paths)
	if err != nil {
		return nil, fmt.Errorf("loading %s at %s: %w", cmp.Or(goos, "default GOOS"), baseRef, err)
	}
	graph := make(map[string][]string, len(paths))
	for _, path := range paths {
		graph[path] = slices.Concat(head[path], base[path])
	}
	return graph, nil
}

// needsBase reports whether baseRef's graph under ctx can attribute a changed
// file that the head graph cannot. A target stops compiling an existing file
// only when a file on its import path changes: one it still compiles, which
// its head graph attributes, or one that compiled under ctx at baseRef and no
// longer does, because it was deleted or its name, build constraint or cgo
// import now excludes it. Only the last kind needs the base. Test files, and Go
// files in directories the go command ignores, are in no graph; other files
// there, such as testdata, can still be embedded. A deleted non-Go file may
// have been an embedded asset; only the base graph can tell.
func needsBase(repoRoot, baseRef string, ctx build.Context, changed []string) (bool, error) {
	for _, file := range changed {
		if strings.HasSuffix(file, "_test.go") || strings.HasSuffix(file, ".go") && slices.ContainsFunc(strings.Split(filepath.Dir(file), "/"), ignoredDir) {
			continue
		}
		head, found, err := headContent(filepath.Join(repoRoot, file))
		if err != nil {
			return false, err
		}
		isGo := strings.HasSuffix(file, ".go")
		switch {
		case found && (!isGo || compiles(ctx, file, head) == nil):
			continue
		case !found && !isGo:
			return true, nil
		}
		base, found, err := blobAt(repoRoot, baseRef, file)
		if err != nil {
			return false, err
		}
		if found && !errors.Is(compiles(ctx, file, base), errExcluded) {
			return true, nil
		}
	}
	return false, nil
}

// ignoredDir reports whether the go command skips packages in a directory
// with this name.
func ignoredDir(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") && name != "."
}

// errExcluded reports a Go file that ctx's build leaves out.
var errExcluded = errors.New("excluded from build")

// compiles reports whether content, as file, is a pure Go file ctx compiles:
// nil if so, errExcluded if ctx leaves it out, and the parse error for a file
// too malformed to tell. build.Context.MatchFile does not check cgo, and the
// graph holds only pure Go files, so importing "C" counts as excluded.
func compiles(ctx build.Context, file string, content []byte) error {
	ctx.OpenFile = func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(content)), nil }
	match, err := ctx.MatchFile(filepath.Dir(file), filepath.Base(file))
	if err != nil {
		return fmt.Errorf("matching %s: %w", file, err)
	}
	if !match {
		return errExcluded
	}
	f, err := parser.ParseFile(token.NewFileSet(), file, content, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", file, err)
	}
	if slices.ContainsFunc(f.Imports, func(spec *ast.ImportSpec) bool { return spec.Path.Value == `"C"` }) {
		return errExcluded
	}
	return nil
}

// headContent returns the working tree's content at path, and whether a file
// is there. A directory that replaced the file counts as absent.
func headContent(path string) ([]byte, bool, error) {
	content, err := os.ReadFile(path)
	if err == nil {
		return content, true, nil
	}
	if info, statErr := os.Stat(path); errors.Is(err, fs.ErrNotExist) || statErr == nil && info.IsDir() {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("reading %s: %w", path, err)
}

// blobAt returns file's content at rev, and whether rev has it as a file.
func blobAt(repoRoot, rev, file string) ([]byte, bool, error) {
	tree, err := exec.Command("git", "-C", repoRoot, "ls-tree", "-z", rev, "--", file).Output()
	if err != nil {
		return nil, false, fmt.Errorf("listing %s at %s: %w", file, rev, err)
	}
	// <mode> SP <type> SP <object> TAB <file> NUL
	fields := strings.Fields(strings.SplitN(string(tree), "\t", 2)[0])
	if len(fields) != 3 || fields[1] != "blob" {
		return nil, false, nil
	}
	blob, err := exec.Command("git", "-C", repoRoot, "cat-file", "blob", fields[2]).Output()
	if err != nil {
		return nil, false, fmt.Errorf("reading %s at %s: %w", file, rev, err)
	}
	return blob, true, nil
}

// platformsOf returns the GOOS values whose graphs make up target's
// dependencies; "" is the analyzer's own GOOS.
func platformsOf(target config.Target) []string {
	if len(target.GOOS) == 0 {
		return []string{""}
	}
	return target.GOOS
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

// stripRules returns a copy of results with Rule fields cleared for
// file-based entries. Triggered-by entries always keep their Rule because
// it names the trigger source — without it the reason is meaningless.
func stripRules(results []eval.Result) []eval.Result {
	out := make([]eval.Result, len(results))
	for i, r := range results {
		files := make([]eval.FileMatch, len(r.Files))
		for j, fm := range r.Files {
			if fm.Reason == "triggered-by" {
				files[j] = fm
			} else {
				files[j] = eval.FileMatch{Path: fm.Path, Reason: fm.Reason}
			}
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
	for i, r := range results {
		if r.Build {
			rebuilds++
		}
		if i > 0 {
			body.WriteByte('\n')
		}
		body.WriteString(formatExplainResult(r))
	}
	fmt.Fprintf(w, "should-build: %d targets evaluated, %d rebuilding\n\n%s\n\n", len(results), rebuilds, body.String())
}

// formatExplainResult formats a single evaluation result as a human-readable
// block. Pure function — no I/O, easy to unit-test.
func formatExplainResult(r eval.Result) string {
	if !r.Build {
		return "  " + r.Target + ": skip"
	}

	var fileCount, triggerCount int
	for _, fm := range r.Files {
		if fm.Reason == "triggered-by" {
			triggerCount++
		} else {
			fileCount++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  %s: rebuild (", r.Target)
	if fileCount > 0 {
		noun := "files"
		if fileCount == 1 {
			noun = "file"
		}
		fmt.Fprintf(&b, "%d %s", fileCount, noun)
		if triggerCount > 0 {
			b.WriteString(", ")
		}
	}
	if triggerCount > 0 {
		noun := "triggers"
		if triggerCount == 1 {
			noun = "trigger"
		}
		fmt.Fprintf(&b, "%d %s", triggerCount, noun)
	}
	b.WriteByte(')')

	for _, fm := range r.Files {
		b.WriteString("\n    ")
		if fm.Reason == "triggered-by" {
			fmt.Fprintf(&b, "triggered by %s", fm.Rule)
		} else {
			b.WriteString(fm.Path)
			b.WriteString("  (")
			b.WriteString(fm.Reason)
			if fm.Rule != "" {
				b.WriteString(": ")
				b.WriteString(fm.Rule)
			}
			b.WriteByte(')')
		}
	}
	return b.String()
}

func writeTable(stdout, stderr io.Writer, results []eval.Result, verbose bool) int {
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "TARGET\tBUILD\tREASON\n")
	for _, r := range results {
		if !r.Build {
			fmt.Fprintf(tw, "%s\tno\t-\n", r.Target)
			continue
		}
		if verbose {
			for _, fm := range r.Files {
				if fm.Reason == "triggered-by" {
					fmt.Fprintf(tw, "%s\tyes\ttriggered by %s\n", r.Target, fm.Rule)
				} else {
					fmt.Fprintf(tw, "%s\tyes\t%s (%s: %s)\n", r.Target, fm.Path, fm.Reason, fm.Rule)
				}
			}
		} else {
			fm := r.Files[0]
			if fm.Reason == "triggered-by" {
				fmt.Fprintf(tw, "%s\tyes\ttriggered by %s\n", r.Target, fm.Rule)
			} else {
				fmt.Fprintf(tw, "%s\tyes\t%s (%s)\n", r.Target, fm.Path, fm.Reason)
			}
		}
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	return 0
}

type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprintf("%v", *s) }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
