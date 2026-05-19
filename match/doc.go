// Package match provides glob pattern matching with template variable
// expansion for should-build config patterns.
//
// Patterns use doublestar syntax: * matches within a path segment,
// ** matches across segments, {a,b} is alternation. The template
// variable {target} is expanded to the target name before matching.
package match
