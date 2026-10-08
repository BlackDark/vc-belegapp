package erkennung

import (
	"bytes"
	"image"
	"image/jpeg"
	"sync"
)

var (
	sampleOnce sync.Once
	sampleJPEG []byte
)

// SampleJPEG is the built-in picture used by the connection test.
func SampleJPEG() []byte {
	sampleOnce.Do(func() {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		var buf bytes.Buffer
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
		sampleJPEG = buf.Bytes()
	})
	return sampleJPEG
}
