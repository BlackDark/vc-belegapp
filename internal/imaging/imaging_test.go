package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func TestNormalizePNGResizeAndStrip(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3000, 10))
	for x := 0; x < 3000; x++ {
		src.Set(x, 0, color.RGBA{R: 200, G: 10, B: 10, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	payload := buf.Bytes()
	out, err := Normalize(payload)
	if err != nil {
		t.Fatal(err)
	}
	if out.Width != 2400 || out.Height != 8 {
		t.Fatalf("size %dx%d", out.Width, out.Height)
	}
	if bytes.Contains(out.JPEG, []byte("Exif")) || bytes.Contains(out.JPEG, []byte("PNG")) {
		t.Fatal("metadata or png signature survived")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out.JPEG))
	if err != nil || cfg.Width != 2400 {
		t.Fatalf("jpeg %v %v", cfg, err)
	}
	if len(out.Thumb) == 0 || out.SHA256 == "" {
		t.Fatal("thumb or hash missing")
	}
}

func TestNormalizeRejects(t *testing.T) {
	if _, err := Normalize([]byte("hello")); err != ErrFormat {
		t.Fatal(err)
	}
	huge := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, huge); err != nil {
		t.Fatal(err)
	}
	// DecodeConfig of a 1x1 image is fine. A claimed bomb is covered by dimensions.
	img := image.NewRGBA(image.Rect(0, 0, 20001, 1))
	buf.Reset()
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if _, err := Normalize(buf.Bytes()); err != ErrTooLarge {
		t.Fatal(err)
	}
}

func TestOrientationRotate(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	src.Set(1, 0, color.RGBA{G: 255, A: 255})
	turned := applyOrientation(src, 6)
	if turned.Bounds().Dx() != 1 || turned.Bounds().Dy() != 2 {
		t.Fatalf("bounds %v", turned.Bounds())
	}
}

func TestNormalizeStripsSampleEXIF(t *testing.T) {
	raw, err := os.ReadFile("testdata/gps.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("GPS")) && !bytes.Contains(raw, []byte("Exif")) {
		t.Fatal("fixture has no exif")
	}
	out, err := Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.JPEG, []byte("GPS")) || bytes.Contains(out.JPEG, []byte("Exif")) {
		t.Fatal("exif survived")
	}
}
