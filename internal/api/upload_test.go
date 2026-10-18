package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every ParseMultipartForm error used to be reported as 413 "too large", which
// sends the operator looking for a size problem on a request that has none: a
// malformed body, a missing boundary or a dropped client is a bad request. Only
// an actual byte-limit violation is 413.
func TestUploadHandlerClassifiesParseErrors(t *testing.T) {
	const cap = 64
	a := &API{UploadMax: cap}

	oversize := multipartBody(t, bytes.Repeat([]byte("a"), cap+8192))
	if status := postUpload(a, oversize); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("an over-limit upload answered %d, want 413", status)
	}

	// A header that names a boundary the body does not carry is malformed, not
	// large.
	garbage := httptest.NewRequest(http.MethodPost, "/api/v1/belegbilder", strings.NewReader("nicht multipart"))
	garbage.Header.Set("Content-Type", "multipart/form-data; boundary=fehlt")
	if status := postUpload(a, garbage); status != http.StatusBadRequest {
		t.Fatalf("a malformed multipart answered %d, want 400", status)
	}
}

func multipartBody(t *testing.T, data []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("datei", "riesig.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/belegbilder", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func postUpload(a *API, req *http.Request) int {
	rec := httptest.NewRecorder()
	a.uploadBild(rec, req)
	return rec.Code
}
