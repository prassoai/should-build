// Package match provides gitignore-style glob matching with template expansion.
//
// All patterns use doublestar syntax: * matches within a path segment,
// ** matches across segments, and {a,b} is alternation.
//
// The ExpandTarget function replaces {target} in patterns with a target name,
// enabling per-target config patterns like "targets/{target}/conf/*.yaml".
package match
