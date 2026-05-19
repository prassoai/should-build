# should-build

Decides whether a component needs to be rebuilt based on dependency-graph
analysis of changes between two git refs.

Given a base ref and a head ref, `should-build` walks the changes in between,
projects them through a declarative config (`should-build.yaml`) and a
language-specific dependency-graph analyzer, and answers a single question per
target: **rebuild, or skip**.

Design: [prassoai/murmuration#1812](https://github.com/prassoai/murmuration/pull/1812).

## Install

```
go install github.com/prassoai/should-build/cmd/should-build@latest
```

## Usage

```
should-build [flags] <base-ref> <head-ref>

Flags:
  --config <path>    Path to config file (default: should-build.yaml)
  --target <name>    Evaluate only this target (repeatable)
  --json             Output JSON (default: human-readable table)
  --quiet            Exit 0 if nothing to rebuild, exit 1 if any target
                     needs rebuilding. No stdout. For CI gates.
  --verbose          Show per-file match details in JSON output.
  --repo <path>      Repository root (default: current directory)
```

### Examples

```bash
# PR CI: which targets changed?
should-build $BASE_SHA $HEAD_SHA

# Single target check for CI gate:
if ! should-build --quiet --target api $BASE_SHA $HEAD_SHA; then
  echo "api needs rebuilding"
fi

# JSON output for scripting:
should-build --json main HEAD
```

## Config

Create `should-build.yaml` at your repo root:

```yaml
global:
  ignore:
    - ".github/**"
    - "docs/**"
    - "**/*.md"
  trigger_all:
    - "go.mod"
    - "go.sum"

# "trigger_all" (default) or "ignore"
unknown_file: trigger_all

targets:
  api:
    path: ./cmd/api          # Go dep-graph root
    include:
      - "k8s/api.yaml"      # extra file patterns
    exclude: []              # opt out of trigger_all patterns

  web:
    lang: none               # no dep-graph analysis
    include:
      - "web/**"

  myservice:
    path: ./cmd/myservice
    include:
      - "targets/{target}/conf/{target}-*.hjson"  # {target} expands to "myservice"
```

### Precedence

For each changed file, against each target:

1. `global.ignore` → file is invisible to all targets.
2. `target.exclude` → target opts out of this file.
3. `target.include` → trigger rebuild.
4. Dependency graph → trigger if file is in the target's transitive Go imports.
5. `global.trigger_all` → trigger rebuild.
6. `unknown_file` policy → `trigger_all` or `ignore`.

## Architecture

```
cmd/should-build/    CLI entry point
config/              YAML schema, loader, validation
match/               Glob matching, {target} template expansion
depgraph/            Analyzer interface + Go implementation
diff/                Git diff (changed file list)
eval/                Pure evaluation engine (config + diff + deps → decisions)
```

The `eval` package is the heart — a pure function over data structures with
no I/O dependencies, making it trivially testable.

## License

Apache-2.0.
