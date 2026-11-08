package normalize

import gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"

func NeedsStdin(argv []string) (bool, string) { return gitnormalize.NeedsStdin(argv) }
