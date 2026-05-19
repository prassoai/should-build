// Package match provides glob matching with template expansion for
// should-build config patterns.
//
// Patterns use doublestar syntax (github.com/bmatcuk/doublestar/v4):
//
//   - * matches any sequence of non-/ characters within a single path segment.
//   - ** matches zero or more path segments (crosses / boundaries).
//   - ? matches any single non-/ character.
//   - {a,b} is alternation.
//
// This is NOT gitignore semantics. In gitignore, a bare "*.md" matches
// files at any depth. Here, "*.md" matches only at the root — use
// "**/*.md" to match .md files in subdirectories. All examples in the
// config schema use ** where directory traversal is needed.
//
// The ExpandTarget function replaces {target} in patterns with a target name,
// enabling per-target config patterns like "targets/{target}/conf/*.yaml".
package match
