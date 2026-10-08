// Package pdf will render the monthly export. M0 only probes the Typst CLI,
// which readiness caches from process start.
package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Probe is the cached result of `typst --version`.
type Probe struct {
	Version string
	Err     error
}

// ProbeBinary runs typst --version once. cacheDir is a writable directory used
// as HOME, TMPDIR, and XDG cache so the probe works when the root filesystem
// is read-only. The caller caches the result.
func ProbeBinary(ctx context.Context, bin, cacheDir string) Probe {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return Probe{Err: errors.New("typst binary path is empty")}
	}
	if cacheDir == "" {
		return Probe{Err: errors.New("typst cache dir is empty")}
	}
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return Probe{Err: fmt.Errorf("typst cache: %w", err)}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = append(os.Environ(),
		"HOME="+cacheDir,
		"TMPDIR="+cacheDir,
		"XDG_CACHE_HOME="+cacheDir,
		"XDG_CONFIG_HOME="+cacheDir,
	)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return Probe{Err: fmt.Errorf("typst --version: %s", text)}
	}
	if text == "" {
		return Probe{Err: errors.New("typst --version returned no output")}
	}
	return Probe{Version: text}
}

// Check returns the cached probe. It does not execute Typst again.
func (p Probe) Check(context.Context) (string, error) {
	if p.Err != nil {
		return "", p.Err
	}
	return p.Version, nil
}
