// Package normalize turns a Git invocation into the canonical operation model.
// All I/O is injected through Invocation; output boundaries must redact secrets.
package normalize

import (
	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
	"github.com/mkusaka/gthok/internal/registry"
)

func Normalize(inv model.Invocation) (res model.Result) {
	defer func() {
		if recover() != nil {
			res = model.NewUnknown(model.LevelNone, model.UnknownParseError, "internal normalizer panic")
			manifest := registry.Manifest()
			res.Registry = model.Registry{Git: manifest.Tag, Commit: manifest.Commit}
			res.Classification.Coverage = model.CoverageConservative
			res.Operations[0].Coverage = model.CoverageConservative
			res.Source = model.Source{Kind: "unknown", Argv: inv.Argv}
		}
	}()
	return gitnormalize.Normalize(inv)
}
