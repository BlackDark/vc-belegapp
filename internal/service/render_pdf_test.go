package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/pdf"
)

// A missing typst binary and a broken document are different operator problems,
// and a single E_PDF_FEHLER hides which one the instance has. The typst probe
// the readiness check already uses is the signal: when it fails, no document
// could have rendered, so the error says the environment is broken rather than
// blaming the document.
func TestRenderPDFDistinguishesMissingTypstFromABadDocument(t *testing.T) {
	ctx := context.Background()

	envBroken := exportSvc(t, &fakePDF{fail: errors.New("renderer must not be reached")})
	var brokenBuf syncBuffer
	envBroken.Log = slog.New(slog.NewJSONHandler(&brokenBuf, nil))
	envBroken.Typst = func(context.Context) (string, error) { return "", errors.New("typst --version: not found") }
	_, err := envBroken.renderPDF(ctx, pdf.Document{}, nil)
	if codeOf(err) != "E_PDF_TYPST" {
		t.Fatalf("a missing typst produced %q (%v), want E_PDF_TYPST", codeOf(err), err)
	}
	assertLogged(t, brokenBuf.String(), "PDF render unavailable")

	docBad := exportSvc(t, &fakePDF{fail: errors.New("typst failed: bad document")})
	var docBuf syncBuffer
	docBad.Log = slog.New(slog.NewJSONHandler(&docBuf, nil))
	docBad.Typst = func(context.Context) (string, error) { return "typst 0.15.1", nil }
	_, err = docBad.renderPDF(ctx, pdf.Document{}, nil)
	if codeOf(err) != "E_PDF_FEHLER" {
		t.Fatalf("a bad document produced %q (%v), want E_PDF_FEHLER", codeOf(err), err)
	}
	assertLogged(t, docBuf.String(), "PDF render failed")
}
