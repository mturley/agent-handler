package main

import "embed"

//go:embed all:web/dist
var EmbeddedWeb embed.FS
