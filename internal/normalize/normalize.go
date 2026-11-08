// Package normalize turns a Git invocation into the canonical operation model.
// All I/O is injected through Invocation; output boundaries must redact secrets.
package normalize

import (
	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
)

func Normalize(inv model.Invocation) (res model.Result) {
	defer func() {
		if recover() != nil {
			res = model.NewUnknown(model.LevelNone, model.UnknownParseError, "internal normalizer panic")
			res.Registry = model.Registry{Git: "v2.56.0", Commit: "a018953688f1b10bddf91bff8747068f5f4746a4"}
			res.Source = model.Source{Kind: "unknown", Argv: inv.Argv}
		}
	}()
	return gitnormalize.Normalize(inv)
}
