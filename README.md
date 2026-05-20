# should-build

Determines which build targets need rebuilding after a code change.

Given a base ref and a head ref, `should-build` walks the changes in between,
projects them through a declarative config (`should-build.yaml`) and a
language-specific dependency-graph analyzer, and answers a single question per
target: rebuild, or skip.

## GitHub Action

The easiest way to use `should-build` in CI is the composite action hosted in
this repo. It downloads a prebuilt binary from the matching GitHub release (with
SHA-256 checksum verification), falling back to building from source if the
binary isn't available for the runner's platform. Outputs are matrix-friendly.

> **Note:** The checkout step must use `fetch-depth: 0` because `should-build`
> runs `git diff` between the base and head commits — a shallow clone won't
> have the base commit.

```yaml
jobs:
  changes:
    runs-on: ubuntu-latest
    outputs:
      targets: ${{ steps.sb.outputs.targets }}
      any: ${{ steps.sb.outputs.any }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: nrwl/nx-set-shas@v4
        id: shas

      - uses: prassoai/should-build@v1
        id: sb
        with:
          base: ${{ steps.shas.outputs.base }}
          head: ${{ steps.shas.outputs.head }}
          # config: should-build.yaml  (default)

  build:
    needs: changes
    if: needs.changes.outputs.any == 'true'
    strategy:
      matrix:
        target: ${{ fromJSON(needs.changes.outputs.targets) }}
    runs-on: ubuntu-latest
    steps:
      - run: echo "Building ${{ matrix.target }}"
```

The `if: ... any == 'true'` guard matters: when no targets need rebuilding,
`targets` is `[]` and `fromJSON` produces a zero-iteration matrix, which
GitHub treats as a workflow error unless the job is skipped.

### Inputs

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `base` | yes | | Base commit SHA |
| `head` | yes | | Head commit SHA |
| `config` | no | `should-build.yaml` | Path to config file, relative to repo root |
| `only` | no | | Comma-separated target names to evaluate, no whitespace (empty = all) |
| `repo` | no | `.` | Repository root path |
| `verbose` | no | `false` | Include per-file match rules in JSON output |

### Outputs

| Output | Description |
|--------|-------------|
| `targets` | JSON array of target names that need rebuilding, e.g. `["api","web"]` |
| `any` | `"true"` if any target needs rebuilding, `"false"` otherwise |
| `json` | Full JSON output from `should-build` |

### Migrating from docker-based usage

If you currently invoke `should-build` via the docker container (as in
`prassoai/back`), replace the `docker run` step with the composite action:

```yaml
# Before (docker):
- name: Determine targets
  run: |
    json=$(docker run --rm -v .:/workspace \
      us-central1-docker.pkg.dev/.../should-build:latest \
      --base $NX_BASE --head $NX_HEAD --repo /workspace \
      --target github.com/prassoai/tools/cmd/api \
      --target github.com/prassoai/tools/cmd/web)
    echo "matrix=$(echo "$json" | jq -cr '[.[] | split("/")[-1]]')" >> "$GITHUB_OUTPUT"

# After (composite action):
- uses: prassoai/should-build@v1
  id: sb
  with:
    base: ${{ env.NX_BASE }}
    head: ${{ env.NX_HEAD }}
    # only: api,web  (optional — omit to evaluate all targets in config)
```

The action handles JSON parsing internally — `outputs.targets` is already the
array of names that need rebuilding.

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
