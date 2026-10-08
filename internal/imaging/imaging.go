// Package imaging decodes uploads and stores a normalised JPEG without metadata.
package imaging

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"

	"github.com/gabriel-vasile/mimetype"
	"github.com/rwcarlsen/goexif/exif"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxEdge      = 2400
	thumbEdge    = 400
	jpegQuality  = 85
	thumbQuality = 75
	maxPixels    = 50_000_000
	maxDimension = 20000
)

// ErrFormat means the upload is not a JPEG, PNG, or WebP.
var ErrFormat = errors.New("imaging: unsupported format")

// ErrTooLarge means the image exceeds the pixel budget.
var ErrTooLarge = errors.New("imaging: image too large")

// Result is the stored JPEG and its thumbnail.
type Result struct {
	JPEG   []byte
	Thumb  []byte
	Width  int
	Height int
	SHA256 string
}

// Normalize sniffs the MIME type, applies EXIF orientation, resizes the long
// edge to at most 2400 px, and re-encodes a JPEG without metadata.
func Normalize(data []byte) (Result, error) {
	mt := mimetype.Detect(data)
	switch mt.String() {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return Result{}, ErrFormat
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, ErrFormat
	}
	if cfg.Width > maxDimension || cfg.Height > maxDimension || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return Result{}, ErrTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, ErrFormat
	}
	img = applyOrientation(img, readOrientation(data))
	img = resize(img, maxEdge)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return Result{}, err
	}
	thumb := resize(img, thumbEdge)
	var thumbBuf bytes.Buffer
	if err := jpeg.Encode(&thumbBuf, thumb, &jpeg.Options{Quality: thumbQuality}); err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(buf.Bytes())
	b := img.Bounds()
	return Result{
		JPEG:   buf.Bytes(),
		Thumb:  thumbBuf.Bytes(),
		Width:  b.Dx(),
		Height: b.Dy(),
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}

func readOrientation(data []byte) int {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return 1
	}
	tag, err := x.Get(exif.Orientation)
	if err != nil {
		return 1
	}
	value, err := tag.Int(0)
	if err != nil || value < 1 || value > 8 {
		return 1
	}
	return value
}

func applyOrientation(src image.Image, orientation int) image.Image {
	switch orientation {
	case 2:
		return flipH(src)
	case 3:
		return rotate(src, 180)
	case 4:
		return flipV(src)
	case 5:
		return rotate(flipH(src), 270)
	case 6:
		return rotate(src, 90)
	case 7:
		return rotate(flipH(src), 90)
	case 8:
		return rotate(src, 270)
	default:
		return src
	}
}

func resize(src image.Image, maxLong int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	long := w
	if h > long {
		long = h
	}
	if long <= maxLong || long == 0 {
		return src
	}
	nw := int(float64(w) * float64(maxLong) / float64(long))
	nh := int(float64(h) * float64(maxLong) / float64(long))
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

func rotate(src image.Image, degrees int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var dst *image.RGBA
	switch degrees {
	case 90:
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	case 180:
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	case 270:
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
	default:
		return src
	}
	return dst
}

func flipH(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipV(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// HashBytes is the hex SHA-256 of the uploaded bytes.
func HashBytes(r io.Reader) (string, []byte, error) {
	var buf bytes.Buffer
	sum := sha256.New()
	if _, err := io.Copy(io.MultiWriter(&buf, sum), r); err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(sum.Sum(nil)), buf.Bytes(), nil
}
