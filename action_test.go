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
// Caller-pinned digests must remain authoritative even if both release assets
// are replaced, and invalid pins must fail before any network request.
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
	if download.Env["INPUT_RELEASE_TAG"] != "${{ inputs.release-tag }}" || download.Env["INPUT_BINARY_SHA256"] != "${{ inputs.binary-sha256 }}" {
		test.Fatal("release pins must enter the script through environment bindings")
	}
	asset := "#!/bin/sh\nexit 0\n"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(asset)))
	for _, scenario := range []struct {
		name     string
		ref      string
		corrupt  bool
		missing  bool
		source   bool
		release  string
		digest   string
		fail     bool
		tamper   bool
		requests int
		arch     string
	}{
		{name: "release", ref: "v0.5", requests: 2},
		{name: "checksum mismatch", ref: "v0.5", corrupt: true, source: true, requests: 2},
		{name: "missing asset", ref: "v0.5", missing: true, source: true, requests: 1},
		{name: "local", source: true},
		{name: "branch", ref: "main", source: true},
		{name: "pinned commit", ref: strings.Repeat("a", 40), source: true},
		{name: "digest-pinned commit", ref: strings.Repeat("a", 40), release: "v0.5", digest: digest, requests: 1},
		{name: "uppercase digest", ref: strings.Repeat("a", 40), release: "v0.5", digest: strings.ToUpper(digest), requests: 1},
		{name: "release checksum is not trusted", ref: strings.Repeat("a", 40), release: "v0.5", digest: digest, corrupt: true, requests: 1},
		{name: "replaced binary and checksum", ref: strings.Repeat("a", 40), release: "v0.5", digest: digest, tamper: true, fail: true, requests: 1},
		{name: "digest mismatch", ref: "main", release: "v0.5", digest: strings.Repeat("0", 64), fail: true, requests: 1},
		{name: "pinned asset unavailable", ref: "main", release: "v0.5", digest: digest, missing: true, fail: true, requests: 1},
		{name: "arm64 pinned asset", ref: strings.Repeat("a", 40), release: "v0.5", digest: digest, arch: "aarch64", requests: 1},
		{name: "missing digest", release: "v0.5", fail: true},
		{name: "missing release", digest: digest, fail: true},
		{name: "short digest", release: "v0.5", digest: "abc", fail: true},
		{name: "nonhex digest", release: "v0.5", digest: strings.Repeat("g", 64), fail: true},
		{name: "floating major rejected", release: "v0", digest: digest, fail: true},
		{name: "tag path rejected", release: "v0.5/../../latest", digest: digest, fail: true},
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
			payload := asset
			if scenario.tamper {
				payload = "#!/bin/sh\necho replaced\n"
			}
			write("asset", payload, 0o644)
			checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
			if scenario.corrupt {
				checksum = strings.Repeat("0", 64)
			}
			write("checksums", checksum+"  should-build-linux-amd64\n", 0o644)
			write("bin/uname", "#!/bin/sh\ncase \"$1\" in -s) echo Linux;; -m) echo \"${ARCH:-x86_64}\";; esac\n", 0o755)
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
  https://github.com/prassoai/should-build/releases/download/v0.5/should-build-linux-arm64) [[ $ARCH == aarch64 ]] && cp "$FIXTURE/asset" "$destination" ;;
  *) exit 1 ;;
esac
`, 0o755)
			command := exec.Command("bash", "-c", download.Run)
			command.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"ACTION_REF="+scenario.ref, "ACTION_REPOSITORY=prassoai/should-build",
				"INPUT_RELEASE_TAG="+scenario.release, "INPUT_BINARY_SHA256="+scenario.digest, "ARCH="+scenario.arch,
				"GITHUB_ACTION_REF=", "GITHUB_ACTION_REPOSITORY=",
				"RUNNER_TEMP="+root, "GITHUB_OUTPUT="+filepath.Join(root, "output"),
				"FIXTURE="+root, fmt.Sprintf("MISSING=%t", scenario.missing))
			if output, err := command.CombinedOutput(); (err != nil) != scenario.fail {
				test.Fatalf("download error = %v, want failure = %t\n%s", err, scenario.fail, output)
			}
			output, err := os.ReadFile(filepath.Join(root, "output"))
			if scenario.fail {
				if !os.IsNotExist(err) {
					test.Fatal("failed pinned downloads must not select source fallback or evaluation")
				}
			} else if err != nil || string(output) != fmt.Sprintf("source=%t\n", scenario.source) {
				test.Fatalf("output = %q, error = %v", output, err)
			}
			binary, err := os.ReadFile(filepath.Join(root, "should-build"))
			if scenario.source || scenario.fail {
				if !os.IsNotExist(err) {
					test.Fatal("source fallback must not leave an unverified executable")
				}
			} else if err != nil || string(binary) != asset {
				test.Fatal("verified release binary must be installed")
			}
			requests, err := os.ReadFile(filepath.Join(root, "requests"))
			if err != nil && !os.IsNotExist(err) {
				test.Fatal(err)
			}
			if len(strings.Fields(string(requests))) != scenario.requests {
				test.Fatalf("requests = %q, want %d downloads", requests, scenario.requests)
			}
			if scenario.digest != "" && strings.Contains(string(requests), "checksums.txt") {
				test.Fatal("pinned downloads must not fetch a release-provided checksum")
			}
			if _, err := os.Stat(filepath.Join(root, "should-build.staging")); !os.IsNotExist(err) {
				test.Fatal("download must not leave a staging binary")
			}
		})
	}
}
