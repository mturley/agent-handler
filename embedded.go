package main

import "embed"

//go:embed skills/*/SKILL.md
var EmbeddedSkills embed.FS

//go:embed hooks/*.sh
var EmbeddedHooks embed.FS

//go:embed rules/*.md
var EmbeddedRules embed.FS
