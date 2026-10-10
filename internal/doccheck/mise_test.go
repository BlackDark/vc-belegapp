package doccheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// .mise.toml pins the local toolchain. Those versions are also the source of
// truth in go.mod, .nvmrc, web/package.json, and ci.yml; a mismatch sends
// developers and CI to different tool versions.
func TestMisePinsMatchSources(t *testing.T) {
	root := repoRoot(t)
	pins := misePins(t, filepath.Join(root, ".mise.toml"))

	for _, want := range []struct{ tool, file, prefix string }{
		{"go", "go.mod", "go "},
		{"node", ".nvmrc", ""},
	} {
		raw, err := os.ReadFile(filepath.Join(root, want.file))
		if err != nil {
			t.Fatal(err)
		}
		if got := firstVersion(string(raw), want.prefix); got != pins[want.tool] {
			t.Errorf("%s: .mise.toml has %s, %s has %s", want.tool, pins[want.tool], want.file, got)
		}
	}

	pnpm := pins["pnpm"]
	for _, file := range []string{"web/package.json", "e2e/package.json"} {
		raw, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		if got := packageManagerPnpm(string(raw)); got != pnpm {
			t.Errorf("pnpm: .mise.toml has %s, %s has %s", pnpm, file, got)
		}
	}

	golangci := pins["golangci-lint"]
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := golangciLintVersion(string(raw)); got != golangci {
		t.Errorf("golangci-lint: .mise.toml has %s, ci.yml has %s", golangci, got)
	}
}

func typstVersion(t *testing.T, root, file string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "ARG TYPST_VERSION="); ok {
			return rest
		}
		if rest, ok := strings.CutPrefix(line, "version="); ok {
			if semver(rest) {
				return rest
			}
		}
	}
	t.Fatalf("%s has no Typst version", file)
	return ""
}

// The two Dockerfiles cannot share the stage: GoReleaser builds
// Dockerfile.goreleaser from a temp context that holds only the binary, so the
// typst stage there cannot COPY scripts/. Both pin the version and both
// checksums instead, and this test keeps the three pins equal.
func TestTypstPinsMatch(t *testing.T) {
	root := repoRoot(t)
	want := typstVersion(t, root, "scripts/install-typst.sh")
	for _, file := range []string{"Dockerfile", "Dockerfile.goreleaser"} {
		if got := typstVersion(t, root, file); got != want {
			t.Errorf("%s pins Typst %s, scripts/install-typst.sh pins %s", file, got, want)
		}
	}
	pins := misePins(t, filepath.Join(root, ".mise.toml"))
	if pins["typst"] != want {
		t.Errorf(".mise.toml pins Typst %s, scripts/install-typst.sh pins %s", pins["typst"], want)
	}
}

func misePins(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		name, version, ok := strings.Cut(line, " = ")
		if !ok || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		name = strings.TrimSpace(name)
		version = strings.TrimSpace(version)
		if idx := strings.Index(version, "#"); idx >= 0 {
			version = strings.TrimSpace(version[:idx])
		}
		version = strings.Trim(version, `"`)
		if version == "" {
			continue
		}
		pins[name] = version
	}
	for _, tool := range []string{"go", "node", "pnpm", "typst", "golangci-lint"} {
		if pins[tool] == "" {
			t.Fatalf(".mise.toml has no pin for %s", tool)
		}
	}
	return pins
}

// firstVersion returns the first semver-looking token on the first line that
// starts with prefix, or the whole first line when prefix is empty.
func firstVersion(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if prefix == "" {
			line = strings.TrimPrefix(line, "v")
			if semver(line) {
				return line
			}
			continue
		}
		if strings.HasPrefix(line, prefix) {
			rest := strings.TrimPrefix(line, prefix)
			rest = strings.TrimPrefix(rest, "toolchain ")
			if idx := strings.Index(rest, "//"); idx >= 0 {
				rest = rest[:idx]
			}
			if semver(strings.TrimSpace(rest)) {
				return strings.TrimSpace(rest)
			}
		}
	}
	return ""
}

func packageManagerPnpm(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, `"packageManager": "pnpm@`); ok {
			return strings.TrimSuffix(rest, `",`)
		}
	}
	return ""
}

func golangciLintVersion(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "version: v"); ok {
			return rest
		}
	}
	return ""
}

func semver(text string) bool {
	parts := strings.Split(text, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return true
}
