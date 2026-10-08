// Package logging configures the process logger.
package logging

import (
	"io"
	"log/slog"
)

// New returns a logger writing to w. level is debug, info, warn, or error.
// format is json or text. Invalid values fall back to info and json; config
// validation rejects them before a server starts.
func New(w io.Writer, level, format string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler)
}
