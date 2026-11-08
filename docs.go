// Package gthok embeds the documentation shipped with the binary.
package gthok

import "embed"

// Docs contains the version-matched user documentation and agent skill.
//
//go:embed README.md docs/*.md skills/gthok/SKILL.md
var Docs embed.FS
