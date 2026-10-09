package doccheck

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Relative links and image paths in Markdown have to resolve in the checkout.
// External URLs are left alone. CI runs this from the go job.
func TestMarkdownLinks(t *testing.T) {
	root := repoRoot(t)
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "dist", "bin", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no markdown files")
	}

	var problems []string
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := stripNonProse(string(raw))
		relFile, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, dest := range destinations(text) {
			if problem := checkDest(root, path, dest); problem != "" {
				problems = append(problems, relFile+": "+problem)
			}
		}
	}
	if len(problems) > 0 {
		t.Fatalf("broken relative links:\n%s", strings.Join(problems, "\n"))
	}
}

func TestCheckDest(t *testing.T) {
	root := repoRoot(t)
	from := filepath.Join(root, "README.md")
	if got := checkDest(root, from, "docs/deployment.md#kubernetes"); got != "" {
		t.Fatal(got)
	}
	if got := checkDest(root, from, "https://example.com/docs"); got != "" {
		t.Fatal(got)
	}
	if got := checkDest(root, from, "docs/no-such-page.md"); !strings.Contains(got, "does not exist") {
		t.Fatalf("missing page: %q", got)
	}
	if got := checkDest(root, from, "../outside.md"); !strings.Contains(got, "escapes") {
		t.Fatalf("escape: %q", got)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"CI":                   "ci",
		"Kubernetes":           "kubernetes",
		"Docker Compose":       "docker-compose",
		"Keine Steuerberatung": "keine-steuerberatung",
		"End-to-end tests":     "end-to-end-tests",
	}
	for heading, want := range cases {
		if got := slug(heading); got != want {
			t.Errorf("%q: got %q, want %q", heading, got, want)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func stripNonProse(text string) string {
	text = fenceRe.ReplaceAllString(text, "")
	text = commentRe.ReplaceAllString(text, "")
	text = inlineCodeRe.ReplaceAllString(text, "")
	return text
}

var (
	fenceRe      = regexp.MustCompile("(?ms)^[ \t]{0,3}```.*?^[ \t]{0,3}```[ \t]*$")
	commentRe    = regexp.MustCompile(`(?s)<!--.*?-->`)
	inlineCodeRe = regexp.MustCompile("`[^`\n]*`")
	attrRe       = regexp.MustCompile(`(?i)\b(?:src|href)\s*=\s*(?:"([^"]+)"|'([^']+)')`)
	headingRe    = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`)
)

func destinations(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != ']' || i+1 >= len(text) || text[i+1] != '(' {
			continue
		}
		raw, next := parseDest(text, i+2)
		if raw != "" {
			out = append(out, raw)
		}
		i = next - 1
	}
	for _, match := range attrRe.FindAllStringSubmatch(text, -1) {
		value := match[1]
		if value == "" {
			value = match[2]
		}
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func parseDest(text string, start int) (string, int) {
	if start >= len(text) {
		return "", start
	}
	if text[start] == '<' {
		end := strings.IndexByte(text[start:], '>')
		if end < 0 {
			return "", len(text)
		}
		return strings.TrimSpace(text[start+1 : start+end]), start + end + 1
	}
	end := strings.IndexByte(text[start:], ')')
	if end < 0 {
		return "", len(text)
	}
	dest := strings.TrimSpace(text[start : start+end])
	if cut := strings.IndexAny(dest, " \t"); cut >= 0 {
		dest = dest[:cut]
	}
	return dest, start + end + 1
}

func checkDest(root, from, dest string) string {
	dest = strings.TrimSpace(dest)
	if dest == "" || isExternal(dest) {
		return ""
	}
	pathPart, frag, _ := strings.Cut(dest, "#")
	pathPart = strings.Split(pathPart, "?")[0]
	pathPart = strings.TrimSpace(pathPart)
	if decoded, err := url.PathUnescape(pathPart); err == nil {
		pathPart = decoded
	}
	target := from
	if pathPart != "" {
		if strings.HasPrefix(pathPart, "/") {
			target = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pathPart, "/")))
		} else {
			target = filepath.Join(filepath.Dir(from), filepath.FromSlash(pathPart))
		}
		target = filepath.Clean(target)
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return dest + " escapes the repository"
		}
		info, err := os.Stat(target)
		if err != nil {
			return dest + " does not exist"
		}
		if frag != "" && !info.IsDir() && !strings.HasSuffix(strings.ToLower(target), ".md") {
			return ""
		}
	}
	if frag == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(target), ".md") {
		return dest + " fragment is not in a markdown file"
	}
	body, err := os.ReadFile(target)
	if err != nil {
		return dest + " fragment target cannot be read"
	}
	if !headingSlugs(string(body))[frag] {
		return dest + " fragment does not match a heading"
	}
	return ""
}

func isExternal(dest string) bool {
	lower := strings.ToLower(dest)
	return strings.Contains(lower, "://") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "//")
}

func headingSlugs(markdown string) map[string]bool {
	text := stripNonProse(markdown)
	counts := map[string]int{}
	out := map[string]bool{}
	for _, match := range headingRe.FindAllStringSubmatch(text, -1) {
		base := slug(match[1])
		if base == "" {
			continue
		}
		name := base
		if n := counts[base]; n > 0 {
			name = base + "-" + strconv.Itoa(n)
		}
		counts[base]++
		out[name] = true
	}
	return out
}

func slug(heading string) string {
	heading = tagRe.ReplaceAllString(heading, "")
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(heading) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if (r == ' ' || r == '-' || r == '_') && !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

var tagRe = regexp.MustCompile(`<[^>]+>`)
