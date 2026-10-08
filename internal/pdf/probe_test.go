package pdf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "typst")
	script := "#!/bin/sh\necho typst 0.15.1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ProbeBinary(context.Background(), bin, filepath.Join(dir, "cache"))
	if got.Err != nil || got.Version != "typst 0.15.1" {
		t.Fatalf("%+v", got)
	}
	version, err := got.Check(context.Background())
	if err != nil || version != "typst 0.15.1" {
		t.Fatalf("check %s %v", version, err)
	}

	missing := ProbeBinary(context.Background(), filepath.Join(dir, "missing"), filepath.Join(dir, "cache"))
	if missing.Err == nil || !strings.Contains(missing.Err.Error(), "typst --version") {
		t.Fatal(missing.Err)
	}
	if _, err := missing.Check(context.Background()); err == nil {
		t.Fatal("expected cached error")
	}
}
