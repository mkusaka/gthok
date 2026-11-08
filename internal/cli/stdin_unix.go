//go:build unix

package cli

import (
	"os"

	native "github.com/mkusaka/gthok/internal/normalize/git"
)

func analysisNeedsStdin(argv []string, stdin *os.File) bool {
	if native.StdinProtocol(argv) {
		info, err := stdin.Stat()
		return err == nil && info.Mode().IsRegular()
	}
	needed, _ := native.NeedsStdin(argv)
	return needed
}
