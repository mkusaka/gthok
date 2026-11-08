package normalize

import "github.com/mkusaka/gthok/internal/normalize/actions"

func KnownActionPattern(glob string) bool { return actions.KnownPattern(glob) }
