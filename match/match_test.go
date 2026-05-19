package match

import (
	"slices"
	"testing"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		pattern, target, want string
	}{
		{"targets/{target}/conf/{target}-*.hjson", "admin", "targets/admin/conf/admin-*.hjson"},
		{"no-template", "api", "no-template"},
		{"{target}", "web", "web"},
	}
	for _, tt := range tests {
		if got := Expand(tt.pattern, tt.target); got != tt.want {
			t.Errorf("Expand(%q, %q) = %q, want %q", tt.pattern, tt.target, got, tt.want)
		}
	}
}

func TestExpandAll(t *testing.T) {
	got := ExpandAll([]string{"{target}/a", "b/{target}"}, "svc")
	want := []string{"svc/a", "b/svc"}
	if !slices.Equal(got, want) {
		t.Errorf("ExpandAll = %v, want %v", got, want)
	}
}

func TestFile(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		// Exact match.
		{"go.mod", "go.mod", true},
		{"go.mod", "go.sum", false},

		// Single star: matches within a segment.
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false},

		// Double star: matches across segments.
		{"**/*.md", "docs/README.md", true},
		{"**/*.md", "README.md", true},
		{"**/*.md", "a/b/c.md", true},
		{"**/*.md", "a/b/c.go", false},

		// Directory prefix.
		{".github/**", ".github/workflows/ci.yaml", true},
		{".github/**", ".github/CODEOWNERS", true},
		{".github/**", "github/foo", false},

		// Alternation.
		{"**/*.{go,proto}", "cmd/main.go", true},
		{"**/*.{go,proto}", "proto/api.proto", true},
		{"**/*.{go,proto}", "docs/readme.md", false},

		// Specific path.
		{"k8s/api.yaml", "k8s/api.yaml", true},
		{"k8s/api.yaml", "k8s/ui.yaml", false},

		// Template-expanded pattern (after Expand).
		{"targets/admin/conf/admin-*.hjson", "targets/admin/conf/admin-nonprod.hjson", true},
		{"targets/admin/conf/admin-*.hjson", "targets/other/conf/other-nonprod.hjson", false},
	}
	for _, tt := range tests {
		if got := File(tt.pattern, tt.path); got != tt.want {
			t.Errorf("File(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestAny(t *testing.T) {
	patterns := []string{"**/*.md", ".github/**", "go.mod"}

	// Matches second pattern.
	if pat, ok := Any(".github/ci.yaml", patterns); !ok || pat != ".github/**" {
		t.Errorf("Any(.github/ci.yaml) = (%q, %v), want (.github/**, true)", pat, ok)
	}

	// No match.
	if pat, ok := Any("cmd/main.go", patterns); ok {
		t.Errorf("Any(cmd/main.go) matched %q, want no match", pat)
	}
}
