package pdf

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/id"
)

//go:embed template/main.typ fonts/Inter-Regular.ttf fonts/Inter-Medium.ttf fonts/Inter-SemiBold.ttf
var assets embed.FS

// ErrFailed means typst did not produce a PDF. The log has stderr.
var ErrFailed = errors.New("pdf: typst failed")

// Renderer produces the monthly PDF.
type Renderer interface {
	Render(ctx context.Context, doc Document, images []Image) ([]byte, error)
}

// CLI is the Typst implementation of Renderer.
type CLI struct {
	Bin     string
	Timeout time.Duration
	Log     *slog.Logger
	Observe func(float64)
}

// Render writes a temp root and runs typst compile --pdf-standard a-2b.
func (c *CLI) Render(ctx context.Context, doc Document, images []Image) ([]byte, error) {
	if c == nil || strings.TrimSpace(c.Bin) == "" {
		return nil, fmt.Errorf("%w: typst binary is empty", ErrFailed)
	}
	started := time.Now()
	defer func() {
		if c.Observe != nil {
			c.Observe(time.Since(started).Seconds())
		}
	}()
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name, err := id.New()
	if err != nil {
		name = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	root := filepath.Join(os.TempDir(), "belegapp-pdf-"+name)
	if err := os.MkdirAll(filepath.Join(root, "fonts"), 0o750); err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(root) }()

	if err := writeAssets(root); err != nil {
		return nil, err
	}
	if err := writeImages(root, images); err != nil {
		return nil, err
	}
	raw, err := marshalDoc(doc)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, "daten.json"), raw, 0o640); err != nil {
		return nil, err
	}
	out := filepath.Join(root, "out.pdf")
	entwurf := "false"
	if doc.Entwurf {
		entwurf = "true"
	}
	cmd := exec.CommandContext(ctx, c.Bin,
		"compile",
		"--root", root,
		"--font-path", filepath.Join(root, "fonts"),
		"--ignore-system-fonts",
		"--pdf-standard", "a-2b",
		"--input", "entwurf="+entwurf,
		"main.typ",
		"out.pdf",
	)
	cmd.Dir = root
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + root,
		"TMPDIR=" + os.TempDir(),
		"XDG_CACHE_HOME=" + os.TempDir(),
		"XDG_CONFIG_HOME=" + root,
		"SOURCE_DATE_EPOCH=" + strconv.FormatInt(doc.ErstelltUnix, 10),
		"LANG=C.UTF-8",
	}
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		msg := trimOutput(stderr)
		if c.Log != nil {
			c.Log.Error("typst compile failed", "err", err.Error(), "stderr", msg)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: timeout", ErrFailed)
		}
		return nil, fmt.Errorf("%w: %s", ErrFailed, msg)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFailed, err.Error())
	}
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		return nil, fmt.Errorf("%w: output is not a PDF", ErrFailed)
	}
	return body, nil
}

func writeAssets(root string) error {
	entries := []struct{ src, dst string }{
		{"template/main.typ", "main.typ"},
		{"fonts/Inter-Regular.ttf", "fonts/Inter-Regular.ttf"},
		{"fonts/Inter-Medium.ttf", "fonts/Inter-Medium.ttf"},
		{"fonts/Inter-SemiBold.ttf", "fonts/Inter-SemiBold.ttf"},
	}
	for _, item := range entries {
		body, err := assets.ReadFile(item.src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, item.dst), body, 0o640); err != nil {
			return err
		}
	}
	return nil
}

func writeImages(root string, images []Image) error {
	if len(images) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(root, "bilder"), 0o750); err != nil {
		return err
	}
	for _, img := range images {
		if img.Name == "" || strings.Contains(img.Name, "..") || filepath.IsAbs(img.Name) {
			return fmt.Errorf("%w: invalid image name", ErrFailed)
		}
		dst := filepath.Join(root, filepath.FromSlash(img.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(dst, img.JPEG, 0o640); err != nil {
			return err
		}
	}
	return nil
}

func marshalDoc(doc Document) ([]byte, error) {
	if doc.Regel == nil {
		doc.Regel = []Paar{}
	}
	if doc.RegelHinweise == nil {
		doc.RegelHinweise = []string{}
	}
	if doc.Belege == nil {
		doc.Belege = []Zeile{}
	}
	if doc.Pruefpunkte == nil {
		doc.Pruefpunkte = []Check{}
	}
	if doc.Fussnoten == nil {
		doc.Fussnoten = []string{}
	}
	if doc.Aenderungen == nil {
		doc.Aenderungen = []Aenderung{}
	}
	if doc.Anhang == nil {
		doc.Anhang = []AnhangSeite{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func trimOutput(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 4000 {
		return text[len(text)-4000:]
	}
	if text == "" {
		return "typst exited"
	}
	return text
}
