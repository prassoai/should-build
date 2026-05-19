// Package config defines the should-build.yaml schema, parses config
// files, and validates their contents.
//
// The config file declares global rules (ignore patterns, trigger-all
// patterns, unknown-file policy) and per-target rules (include/exclude
// patterns, language-specific dep-graph settings).
package config
