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
binary isn't available for the runner's platform. Explicit digest pins instead
require the download to succeed and match the supplied digest. Outputs are
matrix-friendly.

The checkout must contain the base and head commits and the source tree to
analyze. Full history works, but is not required: a PR merge checkout at depth 2
contains its parents, and a shallow main checkout can fetch the chosen base SHA
explicitly. `should-build` compares the two endpoint trees without walking their
ancestry. The example below uses full history for arbitrary refs.

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

      - uses: prassoai/should-build@v0
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
| `setup-go` | no | `true` | Install Go for source fallback; set `false` when the caller provisions Go |
| `release-tag` | no | | Exact `vMAJOR.MINOR` or `vMAJOR.MINOR.PATCH` release tag (optional prerelease suffix); requires `binary-sha256` |
| `binary-sha256` | no | | Expected 64-character hexadecimal SHA-256 for the runner's release binary; requires `release-tag` |

Go targets require the consumer project's Go toolchain and module dependencies
even when should-build itself is downloaded as a prebuilt binary. Callers can
configure that toolchain once and reuse it for source fallback:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
- uses: prassoai/should-build@v0
  with:
    base: ${{ steps.shas.outputs.base }}
    head: ${{ steps.shas.outputs.head }}
    setup-go: "false"
```

With `setup-go: "false"`, the caller's Go must also satisfy this action's
`go.mod` when a source build is needed. Local, branch, and commit-SHA action
references build their exact source unless both release pin inputs are supplied;
version references try checksummed release assets first. The action passes its
ref and repository through explicit step environment bindings so nested shell
steps can resolve the intended release.

### Digest-pinned release binaries

Pin the action code to a reviewed full commit SHA and supply an independently
reviewed binary digest to use a prebuilt executable without trusting a movable
action tag. Replace both placeholders below before using this example:

```yaml
- uses: prassoai/should-build@<reviewed-commit-sha>
  with:
    base: ${{ steps.shas.outputs.base }}
    head: ${{ steps.shas.outputs.head }}
    release-tag: v0.6
    binary-sha256: "<reviewed-64-character-sha256>"
```

The action selects `should-build-<os>-<arch>` for the current runner, so pin the
digest for that specific asset (for example, `should-build-linux-amd64` for a
Linux x64 runner). A multi-platform matrix must supply the corresponding digest
for each platform. The digest identifies the release executable, which can be
from a different revision than the pinned action wrapper.

In this mode, the action downloads only that binary and compares its SHA-256
with the workflow's pin. It does not fetch the release's `checksums.txt` or resolve
a floating major tag. Missing or malformed inputs, download failures, and digest
mismatches fail the action without selecting a source fallback or running the
download. The caller may still apply its own error-handling policy.

Review the digest and binary provenance when updating the pin; fetching an
expected checksum from the same release at runtime would not protect against
replacement of both assets. The action code must also stay SHA-pinned, since a
binary digest cannot protect verification logic loaded from a movable action tag.
Without these inputs, the existing version-tag and source-build behavior remains.

All Go targets share one `go list -json -deps` invocation. Each target retains
its own transitive package closure, including embedded assets, even when target
patterns overlap or multiple targets refer to the same package.

### Outputs

| Output | Description |
|--------|-------------|
| `targets` | JSON array of target names that need rebuilding, e.g. `["api","web"]` |
| `any` | `"true"` if any target needs rebuilding, `"false"` otherwise |
| `json` | Full JSON output from `should-build` |

### Migrating from a docker-based invocation

If you currently invoke `should-build` via a docker image, replace the
`docker run` step with the composite action:

```yaml
# Before (docker):
- name: Determine targets
  run: |
    json=$(docker run --rm -v .:/workspace \
      ghcr.io/your-org/should-build:latest \
      --base $NX_BASE --head $NX_HEAD --repo /workspace)
    echo "matrix=$(echo "$json" | jq -cr '[.[] | split("/")[-1]]')" >> "$GITHUB_OUTPUT"

# After (composite action):
- uses: prassoai/should-build@v0
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
| `--version` | Print version and exit |

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

### Selection modes

`should-build` fails open by default: `global.trigger_all` and the
`unknown_file` fallback reach every target, so an unrecognized change rebuilds
everything. That is the right trade for a build measured in minutes and the
wrong one for a target measured in machine-hours — a multi-cloud VM image bake
selected by an orphaned test fixture is pure waste.

Set `selection: explicit` on such a target to opt it out of exactly those two
repo-wide rules:

```yaml
targets:
  base-images:
    lang: none
    selection: explicit    # default: fail_open
    include:
      - "terraform/packer/**"
    exclude:
      - "terraform/packer/tests/**"
      - "terraform/packer/**/*.md"
```

An explicit target is selected only by rules that name its own inputs:

| Rule | `fail_open` (default) | `explicit` |
|------|----------------------|------------|
| `global.ignore` | file invisible | file invisible |
| `target.exclude` | opts out | opts out |
| `target.include` | selects | selects |
| Dependency graph | selects | selects |
| `global.trigger_all` | selects | **never selects** |
| `unknown_file: trigger_all` | selects (orphans) | **never selects** |
| `target.triggers` from another target | selects | selects |

The mode is per-target. Every other target keeps its fail-open behavior, and
`global.ignore` is unchanged — there is no global switch to flip.

An explicit target still *claims* its `include` patterns for orphan
classification, so declaring an expensive target's inputs also stops those
files reaching every other target's `unknown_file` fallback.

Because the failure direction of `explicit` is a target that silently never
builds, `should-build` rejects an explicit target nothing can select: no
`include` patterns, no Go dependency graph, and no incoming trigger is a
config error, reported at parse time.

### Target triggers

A target can declare `triggers:` — a list of other targets that must also
build whenever it builds. Use this when deploying target A requires target B
to exist at the same version (e.g. a control plane and its matching VM image):

```yaml
targets:
  murmur-control:
    path: ./cmd/murmur-control
    triggers:
      - murmur-vm
      - murmur
  murmur-vm:
    path: ./cmd/murmur-vm
  murmur:
    path: ./cmd/murmur
```

Triggers propagate transitively: if A triggers B and B triggers C, building A
also builds B and C. Cycles are a configuration error — `should-build` rejects
them at parse time. Targets that already build from their own rules are not
given a redundant `triggered-by` entry.

In the JSON output, triggered targets appear with reason `"triggered-by"` and
rule set to the source target name:

```json
{
  "target": "murmur-vm",
  "build": true,
  "files": [{ "reason": "triggered-by", "rule": "murmur-control" }]
}
```

### Evaluation precedence

**Evaluation precedence** (per file, per target):

1. `global.ignore` — file invisible to all targets
2. `target.exclude` — target opts out
3. `target.include` — triggers target
4. Dependency graph — file is in a package the target imports
5. `global.trigger_all` — triggers all non-excluded targets
6. `unknown_file` — fallback policy for **orphan** files only
7. `target.triggers` — after per-target evaluation, propagate builds transitively

Steps 5 and 6 are the repo-wide fallbacks — they select a target without
naming any of its inputs. A target with `selection: explicit` skips both; see
[Selection modes](#selection-modes).

A file is an **orphan** only if *no* target accounts for it — it matches no
target's `include`, is in no target's dependency graph, and matches no
`global.trigger_all` pattern. The `unknown_file: trigger_all` safety net fires
for orphans alone. A file that some target depends on is not an orphan, so it
rebuilds only the targets that actually reference it (plus their `triggers`) —
a change scoped to one service's files does not rebuild every target.

**Glob syntax.** Patterns use [doublestar](https://github.com/bmatcuk/doublestar)
globs, not gitignore semantics. `*` matches within a single path segment;
use `**` to cross directory boundaries. `*.md` matches `README.md` but NOT
`docs/README.md` — write `**/*.md` for that.

The `{target}` template variable in `include`/`exclude` patterns expands to
the target's key name: `targets/{target}/conf/*.yaml` becomes
`targets/api/conf/*.yaml` for the `api` target.

## License

Apache-2.0.
