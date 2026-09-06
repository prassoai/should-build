package shouldbuild_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestActionDownload verifies ref selection, checksums, and source fallback by
// running the action's download script against local release-asset fixtures.
func TestActionDownload(test *testing.T) {
	contents, err := os.ReadFile("action.yml")
	if err != nil {
		test.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []struct {
				ID  string            `yaml:"id"`
				Env map[string]string `yaml:"env"`
				Run string            `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(contents, &action); err != nil {
		test.Fatal(err)
	}
	download := action.Runs.Steps[0]
	if download.ID != "download" || download.Env["ACTION_REF"] != "${{ github.action_ref }}" || download.Env["ACTION_REPOSITORY"] != "${{ github.action_repository }}" {
		test.Fatal("download must receive explicit action context through its environment")
	}
	for _, scenario := range []struct {
		name    string
		ref     string
		corrupt bool
		missing bool
		source  bool
	}{
		{name: "release", ref: "v0.5"},
		{name: "checksum mismatch", ref: "v0.5", corrupt: true, source: true},
		{name: "missing asset", ref: "v0.5", missing: true, source: true},
		{name: "local", source: true},
		{name: "branch", ref: "main", source: true},
		{name: "pinned commit", ref: strings.Repeat("a", 40), source: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			root := test.TempDir()
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0o755); err != nil {
				test.Fatal(err)
			}
			write := func(name, contents string, mode os.FileMode) {
				test.Helper()
				if err := os.WriteFile(filepath.Join(root, name), []byte(contents), mode); err != nil {
					test.Fatal(err)
				}
			}
			asset := "#!/bin/sh\nexit 0\n"
			write("asset", asset, 0o644)
			checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(asset)))
			if scenario.corrupt {
				checksum = strings.Repeat("0", 64)
			}
			write("checksums", checksum+"  should-build-linux-amd64\n", 0o644)
			write("bin/uname", "#!/bin/sh\ncase \"$1\" in -s) echo Linux;; -m) echo x86_64;; esac\n", 0o755)
			write("bin/curl", `#!/bin/bash
set -euo pipefail
destination=''
while [[ $# -gt 0 ]]; do
  if [[ $1 == -o ]]; then destination=$2; shift; fi
  url=$1
  shift
done
printf '%s\n' "$url" >> "$FIXTURE/requests"
[[ $MISSING == false ]] || exit 22
case "$url" in
  https://github.com/prassoai/should-build/releases/download/v0.5/checksums.txt) cp "$FIXTURE/checksums" "$destination" ;;
  https://github.com/prassoai/should-build/releases/download/v0.5/should-build-linux-amd64) cp "$FIXTURE/asset" "$destination" ;;
  *) exit 1 ;;
esac
`, 0o755)
			command := exec.Command("bash", "-c", download.Run)
			command.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"ACTION_REF="+scenario.ref, "ACTION_REPOSITORY=prassoai/should-build",
				"GITHUB_ACTION_REF=", "GITHUB_ACTION_REPOSITORY=",
				"RUNNER_TEMP="+root, "GITHUB_OUTPUT="+filepath.Join(root, "output"),
				"FIXTURE="+root, fmt.Sprintf("MISSING=%t", scenario.missing))
			if output, err := command.CombinedOutput(); err != nil {
				test.Fatalf("download failed: %v\n%s", err, output)
			}
			output, err := os.ReadFile(filepath.Join(root, "output"))
			if err != nil || string(output) != fmt.Sprintf("source=%t\n", scenario.source) {
				test.Fatalf("output = %q, error = %v", output, err)
			}
			binary, err := os.ReadFile(filepath.Join(root, "should-build"))
			if scenario.source {
				if !os.IsNotExist(err) {
					test.Fatal("source fallback must not leave an unverified executable")
				}
			} else if err != nil || string(binary) != asset {
				test.Fatal("verified release binary must be installed")
			}
			if scenario.ref != "v0.5" {
				if _, err := os.Stat(filepath.Join(root, "requests")); !os.IsNotExist(err) {
					test.Fatal("local, branch, and SHA references must not download a different release")
				}
			}
		})
	}
}
