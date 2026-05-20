package match

import "testing"

// TestMatchAnyGlobs exercises glob semantics through MatchAny (the only
// matching function in the public API). Each case uses a single-element
// pattern slice so the test isolates one glob behavior at a time.
func TestMatchAnyGlobs(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		// Basic single-star matches within a segment.
		{name: "star extension", pattern: "*.go", path: "foo.go", want: true},
		{name: "star no match", pattern: "*.go", path: "foo.rs", want: false},
		{name: "star in dir", pattern: "cmd/*", path: "cmd/foo", want: true},
		{name: "star no deep", pattern: "cmd/*", path: "cmd/foo/bar", want: false},

		// CRITICAL: * does NOT cross / boundaries. This is the key difference
		// from gitignore semantics, where bare "*.md" matches at any depth.
		// Users must write "**/*.md" for depth-crossing matches.
		{name: "star does not cross slash", pattern: "*.md", path: "docs/README.md", want: false},
		{name: "star does not cross nested", pattern: "*.go", path: "cmd/app/main.go", want: false},

		// Double-star matches across segments.
		{name: "doublestar ext", pattern: "**/*.go", path: "a/b/c.go", want: true},
		{name: "doublestar root", pattern: "**/*.go", path: "main.go", want: true},
		{name: "doublestar prefix", pattern: "docs/**", path: "docs/api/v1.md", want: true},
		{name: "doublestar no match", pattern: "docs/**", path: "src/main.go", want: false},
		{name: "doublestar md", pattern: "**/*.md", path: "docs/README.md", want: true},

		// Alternation with braces.
		{name: "alternation match first", pattern: "*.{go,proto}", path: "foo.go", want: true},
		{name: "alternation match second", pattern: "*.{go,proto}", path: "foo.proto", want: true},
		{name: "alternation no match", pattern: "*.{go,proto}", path: "foo.rs", want: false},

		// Question mark matches single non-/ character.
		{name: "question mark", pattern: "?.go", path: "a.go", want: true},
		{name: "question mark no match", pattern: "?.go", path: "ab.go", want: false},
		{name: "question mark no slash", pattern: "?/a.go", path: "x/a.go", want: true},

		// Exact path match.
		{name: "exact", pattern: "go.mod", path: "go.mod", want: true},
		{name: "exact no match", pattern: "go.mod", path: "go.sum", want: false},
		{name: "exact with dir", pattern: "cmd/api/main.go", path: "cmd/api/main.go", want: true},

		// Nested doublestar.
		{name: "nested doublestar", pattern: "**/testdata/**", path: "pkg/foo/testdata/x.json", want: true},
		{name: "nested doublestar root", pattern: "**/testdata/**", path: "testdata/x.json", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := MatchAny([]string{tt.pattern}, tt.path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("MatchAny([%q], %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

// TestExpandTarget verifies that {target} is replaced everywhere in the pattern.
func TestExpandTarget(t *testing.T) {
	tests := []struct {
		pattern string
		target  string
		want    string
	}{
		{"targets/{target}/conf/{target}-*.hjson", "myservice", "targets/myservice/conf/myservice-*.hjson"},
		{"no-template", "foo", "no-template"},
		{"{target}", "bar", "bar"},
	}
	for _, tt := range tests {
		got := ExpandTarget(tt.pattern, tt.target)
		if got != tt.want {
			t.Errorf("ExpandTarget(%q, %q) = %q, want %q", tt.pattern, tt.target, got, tt.want)
		}
	}
}

// TestMatchAnyShortCircuit verifies that MatchAny returns the first matching
// pattern and stops.
func TestMatchAnyShortCircuit(t *testing.T) {
	patterns := []string{"docs/**", "**/*.md", "go.mod"}

	// Matches first pattern.
	ok, pat, err := MatchAny(patterns, "docs/api.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || pat != "docs/**" {
		t.Errorf("got ok=%v pat=%q, want ok=true pat=%q", ok, pat, "docs/**")
	}

	// Matches second pattern (not first).
	ok, pat, err = MatchAny(patterns, "src/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || pat != "**/*.md" {
		t.Errorf("got ok=%v pat=%q, want ok=true pat=%q", ok, pat, "**/*.md")
	}

	// No match.
	ok, _, err = MatchAny(patterns, "cmd/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected no match")
	}

	// Empty pattern list.
	ok, _, err = MatchAny(nil, "anything")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected no match for nil patterns")
	}
}

func TestValidatePattern(t *testing.T) {
	if err := ValidatePattern("**/*.go"); err != nil {
		t.Errorf("valid pattern rejected: %v", err)
	}
	if err := ValidatePattern("[invalid"); err == nil {
		t.Error("expected error for malformed pattern")
	}
}
