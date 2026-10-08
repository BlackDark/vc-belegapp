package pdf

import (
	"context"
	"errors"
)

// ErrNichtVerfuegbar means the monthly PDF renderer is not part of this milestone.
var ErrNichtVerfuegbar = errors.New("pdf: renderer not available")

// MonatInput is the data a later renderer will receive.
type MonatInput struct {
	Monat string
	Draft bool
}

// Renderer produces the monthly PDF. M3 supplies the Typst implementation.
type Renderer interface {
	RenderMonat(ctx context.Context, in MonatInput) ([]byte, error)
}

// Unavailable is the M1 stand-in.
type Unavailable struct{}

// RenderMonat returns ErrNichtVerfuegbar.
func (Unavailable) RenderMonat(context.Context, MonatInput) ([]byte, error) {
	return nil, ErrNichtVerfuegbar
}
