package config

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// docs/configuration.md is the operator reference. This fails CI when a
// BELEGAPP_ variable exists in code and is absent from that table.
func TestConfigurationDocTable(t *testing.T) {
	known, defaults := envFromCode(t)
	rows := configDocRows(t)

	var missing []string
	for _, name := range known {
		row, ok := rows[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if def := defaults[name]; def != "" && !strings.Contains(row, "`"+def+"`") {
			t.Errorf("%s: default cell %q does not contain `%s`", name, row, def)
		}
	}
	if len(missing) > 0 {
		t.Errorf("docs/configuration.md config table is missing: %s", strings.Join(missing, ", "))
	}
	for name := range rows {
		if !contains(known, name) {
			t.Errorf("docs/configuration.md config table lists %s, which is not in internal/config", name)
		}
	}
}

func TestReadmeGoVersion(t *testing.T) {
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^go (\d+\.\d+\.\d+)\s*$`).FindSubmatch(mod)
	if match == nil {
		t.Fatal("go.mod has no go version")
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	version := string(match[1])
	if !strings.Contains(string(readme), version) {
		t.Errorf("README does not mention Go %s from go.mod", version)
	}
}

func envFromCode(t *testing.T) ([]string, map[string]string) {
	t.Helper()
	defaults := map[string]string{}
	var names []string
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := field.Tag.Get("env")
		if name == "" {
			continue
		}
		names = append(names, name)
		defaults[name] = field.Tag.Get("envDefault")
	}
	for _, base := range secretFileBases(t) {
		names = append(names, base+"_FILE")
	}
	return names, defaults
}

func secretFileBases(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	start := strings.Index(text, "func applySecretFiles")
	if start < 0 {
		t.Fatal("applySecretFiles not found")
	}
	rest := text[start:]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		t.Fatal("applySecretFiles has no following func")
	}
	block := rest[:end]
	found := regexp.MustCompile(`"(BELEGAPP_[A-Z0-9_]+)"`).FindAllStringSubmatch(block, -1)
	if len(found) == 0 {
		t.Fatal("applySecretFiles lists no variables")
	}
	bases := make([]string, 0, len(found))
	for _, match := range found {
		bases = append(bases, match[1])
	}
	return bases
}

func configDocRows(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	const startMark = "<!-- config-env-start -->"
	const endMark = "<!-- config-env-end -->"
	start := strings.Index(text, startMark)
	end := strings.Index(text, endMark)
	if start < 0 || end < 0 || end < start {
		t.Fatal("docs/configuration.md is missing config-env markers")
	}
	section := text[start+len(startMark) : end]
	nameRe := regexp.MustCompile(`BELEGAPP_[A-Z0-9_]+`)
	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		names := nameRe.FindAllString(cells[1], -1)
		if len(names) == 0 {
			continue
		}
		for _, name := range names {
			if _, ok := rows[name]; ok {
				t.Errorf("duplicate config table row for %s", name)
			}
			rows[name] = cells[2]
		}
	}
	if len(rows) == 0 {
		t.Fatal("docs/configuration.md config table has no BELEGAPP_ rows")
	}
	return rows
}

func contains(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}
