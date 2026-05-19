package config

import "example.com/testmod/internal/util"

// Load is a stub for testing transitive dependency resolution.
func Load() { util.Helper() }
