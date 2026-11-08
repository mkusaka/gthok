// Package git classifies Git invocations. Command semantics are added separately.
package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/gitargs"
	"github.com/mkusaka/gthok/internal/model"
)

func Normalize(inv model.Invocation) model.Result {
	expanded, alias, reason := ExpandAlias(inv.Argv, inv.Aliases)
	g, err := gitargs.Parse(expanded)
	level := model.LevelNone
	if g.Command != "" {
		level = model.LevelL1
	}
	if reason == model.UnknownNone {
		reason = model.UnknownCommand
	}
	detail := "Git command semantics are not installed"
	if err != nil {
		reason, detail = model.UnknownParseError, err.Error()
	}
	res := model.NewUnknown(level, reason, detail)
	res.Registry = model.Registry{Git: "v2.56.0", Commit: "a018953688f1b10bddf91bff8747068f5f4746a4"}
	res.Source = model.Source{Kind: "command", Argv: expanded, Alias: alias, Global: g.Tokens}
	if g.Command != "" {
		res.Operations[0].Origin.Command = []string{g.Command}
	}
	return res
}

// shortcut: expand once with whitespace splitting, until the full Git alias parser lands.
func ExpandAlias(argv []string, aliases map[string]string) ([]string, string, model.UnknownReason) {
	g, err := gitargs.Parse(argv)
	if err != nil || g.Command == "" || IsBuiltinCommand(g.Command) {
		return argv, "", model.UnknownNone
	}
	value, ok := aliases[g.Command]
	if !ok {
		return argv, "", model.UnknownNone
	}
	if strings.HasPrefix(value, "!") {
		return argv, g.Command, model.UnknownShellAlias
	}
	out := append([]string(nil), g.Tokens...)
	out = append(out, strings.Fields(value)...)
	return append(out, g.Args...), g.Command, model.UnknownNone
}

func IsBuiltinCommand(name string) bool {
	// shortcut: bootstrap builtin list, replace with the generated command registry.
	return strings.Contains(" add am archive bisect blame branch bundle cat-file check-attr check-ignore check-mailmap check-ref-format checkout cherry cherry-pick clean clone column commit commit-tree config count-objects credential describe diff diff-files diff-index diff-tree fast-export fast-import fetch fetch-pack filter-branch fmt-merge-msg for-each-ref format-patch fsck gc get-tar-commit-id grep hash-object help index-pack init interpret-trailers log ls-files ls-remote ls-tree mailinfo mailsplit maintenance merge merge-base merge-file merge-index merge-tree mktag mktree mv name-rev notes pack-objects pack-redundant pack-refs patch-id prune prune-packed pull push range-diff read-tree rebase reflog refs remote repack replace request-pull rerere reset restore rev-list rev-parse revert rm send-email send-pack shortlog show show-branch show-index show-ref sparse-checkout stage stash status stripspace submodule switch symbolic-ref tag unpack-file unpack-objects update-index update-ref update-server-info var verify-commit verify-pack verify-tag version whatchanged worktree write-tree ", " "+name+" ")
}

func NeedsStdin(argv []string) (bool, string) {
	g, _ := gitargs.Parse(argv)
	for i, arg := range g.Args {
		if (arg == "-F" || arg == "--file") && i+1 < len(g.Args) && g.Args[i+1] == "-" {
			return true, "commit-message"
		}
	}
	// Bidirectional protocols must never be read ahead from a pipe.
	return false, ""
}

func ActionRoutes() map[string][]string { return map[string][]string{} }
