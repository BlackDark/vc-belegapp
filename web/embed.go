// Package web embeds the Vite build (web/dist) into the server binary.
package web

import "embed"

// Dist is the production frontend. Paths are rooted at dist/.
//
//go:embed all:dist
var Dist embed.FS
