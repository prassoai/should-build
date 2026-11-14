package depgraph

import (
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// TestGoDepsNativeInputs uses real package loading and literal expectations so
// omitted go list fields cannot also disappear from the test's expected set.
func TestGoDepsNativeInputs(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	otherOS := "linux"
	if runtime.GOOS == otherOS {
		otherOS = "windows"
	}
	for name, content := range map[string]string{
		"go.mod":                                          "module example.com/test\n\ngo 1.23\nrequire example.com/external v0.0.0\nreplace example.com/external => ../external\n",
		"cmd/one/main.go":                                 "package main\nimport (_ \"example.com/test/internal/bridge\"; _ \"example.com/external\")\nfunc main() {}\n",
		"cmd/two/main.go":                                 "package main\nimport (_ \"example.com/test/internal/bridge\"; _ \"example.com/test/internal/private\")\nfunc main() {}\n",
		"cmd/other/main.go":                               "package main\nfunc main() {}\n",
		"internal/bridge/bridge.go":                       "package bridge\nimport _ \"example.com/test/internal/native\"\n",
		"internal/private/private.go":                     "package private\n",
		"internal/native/native.go":                       "package native\n/*\n#include \"value.h\"\n*/\nimport \"C\"\n",
		"internal/native/fallback.go":                     "//go:build !cgo\n\npackage native\n",
		"internal/native/data.go":                         "package native\nimport _ \"embed\"\n//go:embed data.txt\nvar Data string\n",
		"internal/native/data.txt":                        "embedded\n",
		"internal/native/value.c":                         "//go:build cgo\n\n#include \"value.h\"\nint value(void) { return 42; }\n",
		"internal/native/value.h":                         "int value(void);\n",
		"internal/native/extra.cc":                        "//go:build cgo\n\n",
		"internal/native/extra.m":                         "//go:build cgo\n\n",
		"internal/native/extra.f":                         "//go:build cgo\n\n",
		"internal/native/extra.swig":                      "//go:build cgo\n\n%module extra\n",
		"internal/native/extra.swigcxx":                   "//go:build cgo\n\n%module extra\n",
		"internal/native/tagged.c":                        "//go:build nativeextra\n\n",
		"internal/native/platform_" + runtime.GOOS + ".c": "//go:build cgo\n\n",
		"internal/native/platform_" + otherOS + ".c":      "ignored\n",
		"internal/native/ignored.go":                      "//go:build ignore\n\npackage native\n",
		"internal/native/ignored.c":                       "//go:build ignore\n\n",
		"internal/native/native_test.go":                  "package native\nimport _ \"example.com/test/testonly\"\n",
		"internal/native/external_test.go":                "package native_test\n",
		"internal/native/unused.txt":                      "not embedded\n",
		"internal/native/headers/arbitrary.h":             "not returned by go list\n",
		"testonly/testonly.go":                            "package testonly\n",
	} {
		writeFile(t, filepath.Join(root, name), content)
	}
	writeFile(t, filepath.Join(parent, "external/go.mod"), "module example.com/external\n\ngo 1.23\n")
	writeFile(t, filepath.Join(parent, "external/native.go"), "package external\nimport \"C\"\n")
	writeFile(t, filepath.Join(parent, "external/value.c"), "int external_value;\n")
	writeFile(t, filepath.Join(parent, "external/fallback.go"), "//go:build !cgo\n\npackage external\n")

	for _, tc := range []struct {
		name, cgo, flags string
		want             []string
	}{
		{"cgo", "1", "", []string{
			"internal/native/data.go", "internal/native/data.txt", "internal/native/extra.cc",
			"internal/native/extra.f", "internal/native/extra.m", "internal/native/extra.swig",
			"internal/native/extra.swigcxx", "internal/native/native.go",
			"internal/native/platform_" + runtime.GOOS + ".c", "internal/native/value.c", "internal/native/value.h",
		}},
		{"tagged", "1", "-tags=nativeextra", []string{
			"internal/native/data.go", "internal/native/data.txt", "internal/native/extra.cc",
			"internal/native/extra.f", "internal/native/extra.m", "internal/native/extra.swig",
			"internal/native/extra.swigcxx", "internal/native/native.go",
			"internal/native/platform_" + runtime.GOOS + ".c", "internal/native/tagged.c", "internal/native/value.c", "internal/native/value.h",
		}},
		{"cgo-disabled", "0", "", []string{
			"internal/native/data.go", "internal/native/data.txt", "internal/native/fallback.go", "internal/native/value.h",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CGO_ENABLED", tc.cgo)
			t.Setenv("GOFLAGS", tc.flags)
			got, err := (Go{}).DepsAll(root, []string{"./cmd/one", "./cmd/two", "./cmd/other", "./cmd/...", "./internal/native"})
			if err != nil {
				t.Fatal(err)
			}
			for pattern, own := range map[string][]string{
				"./cmd/one":         {"cmd/one/main.go", "internal/bridge/bridge.go"},
				"./cmd/two":         {"cmd/two/main.go", "internal/bridge/bridge.go", "internal/private/private.go"},
				"./cmd/...":         {"cmd/one/main.go", "cmd/other/main.go", "cmd/two/main.go", "internal/bridge/bridge.go", "internal/private/private.go"},
				"./internal/native": nil,
			} {
				want := append(slices.Clone(tc.want), own...)
				slices.Sort(want)
				if !slices.Equal(got[pattern], want) {
					t.Errorf("%s: got %v, want %v", pattern, got[pattern], want)
				}
			}
			if want := []string{"cmd/other/main.go"}; !slices.Equal(got["./cmd/other"], want) {
				t.Errorf("isolated target: got %v, want %v", got["./cmd/other"], want)
			}
		})
	}
}

// TestGoDepsAssemblyInputs protects native inputs that remain valid without
// cgo. These list-only fixtures need no architecture-specific object encoding.
func TestGoDepsAssemblyInputs(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOFLAGS", "")
	root := t.TempDir()
	otherArch := "amd64"
	if runtime.GOARCH == otherArch {
		otherArch = "arm64"
	}
	for name, content := range map[string]string{
		"go.mod":                       "module example.com/test\n\ngo 1.23\n",
		"asm.go":                       "package asm\n",
		"value.s":                      "// Go assembly\n",
		"value.h":                      "// Assembly header\n",
		"value.syso":                   "",
		"ignored.s":                    "//go:build ignore\n\n",
		"ignored.h":                    "//go:build ignore\n\n",
		"wrong_" + otherArch + ".s":    "",
		"wrong_" + otherArch + ".syso": "",
	} {
		writeFile(t, filepath.Join(root, name), content)
	}
	got, err := (Go{}).Deps(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"asm.go", "value.h", "value.s", "value.syso"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
