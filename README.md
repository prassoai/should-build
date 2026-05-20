# should-build

Determines which build targets need rebuilding after a code change.

Given a base ref and a head ref, `should-build` walks the changes in between,
projects them through a declarative config (`should-build.yaml`) and a
language-specific dependency-graph analyzer, and answers a single question per
target: rebuild, or skip.

## Install

```
go install github.com/prassoai/should-build/cmd/should-build@latest
```

## Usage

```
should-build [flags] <base-ref> <head-ref>
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to config file (default: `should-build.yaml`, relative to `--repo`) |
| `--target <name>` | Evaluate only this target (repeatable) |
| `--json` | Output JSON |
| `--quiet` | Exit 0 = nothing to rebuild, exit 1 = rebuild needed. No stdout. |
| `--verbose` | Show per-file match rules |
| `--repo <path>` | Repository root (default: `.`) |

**Examples:**

```bash
# PR CI: which targets changed between base and head?
should-build $BASE_SHA $HEAD_SHA

# Human-readable table
should-build main HEAD

# JSON for scripting
should-build --json main HEAD

# CI gate: does this target need a rebuild?
if ! should-build --quiet --target api main HEAD; then
  echo "api needs rebuilding"
fi
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

unknown_file: trigger_all  # or "ignore"

targets:
  api:
    path: ./cmd/api        # Go dep-graph root
    include:
      - "k8s/api.yaml"
    exclude: []

  web:
    lang: none             # no dep graph; patterns only
    include:
      - "web/**"
```

**Evaluation precedence** (per file, per target):

1. `global.ignore` — file invisible to all targets
2. `target.exclude` — target opts out
3. `target.include` — triggers target
4. Dependency graph — file is in a package the target imports
5. `global.trigger_all` — triggers all non-excluded targets
6. `unknown_file` — fallback policy

**Glob syntax.** Patterns use [doublestar](https://github.com/bmatcuk/doublestar)
globs, not gitignore semantics. `*` matches within a single path segment;
use `**` to cross directory boundaries. `*.md` matches `README.md` but NOT
`docs/README.md` — write `**/*.md` for that.

The `{target}` template variable in `include`/`exclude` patterns expands to
the target's key name: `targets/{target}/conf/*.yaml` becomes
`targets/api/conf/*.yaml` for the `api` target.

## Design

[prassoai/murmuration#1812](https://github.com/prassoai/murmuration/pull/1812)

## License

Apache-2.0.
