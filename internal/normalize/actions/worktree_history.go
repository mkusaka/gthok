package actions

import "github.com/mkusaka/gthok/internal/model"

func init() {
	for family, modes := range map[string][]string{
		"worktree.reset":           {"soft", "mixed", "hard", "merge", "keep", "paths", "patch"},
		"worktree.restore":         {"paths"},
		"worktree.checkout":        {"switch", "create", "reset", "orphan", "detach", "paths"},
		"worktree.switch":          {"switch", "create", "reset", "orphan", "detach"},
		"index.add":                {"update", "refresh"},
		"index.rm":                 {"remove"},
		"worktree.mv":              {"move"},
		"stash":                    {"push", "pop", "apply", "drop", "clear", "branch", "store", "create", "export", "import"},
		"history.commit":           {"create", "amend"},
		"history.merge":            {"start", "abort", "continue", "quit", "squash"},
		"history.rebase":           {"start", "continue", "abort", "skip", "quit", "edit-todo"},
		"history.cherry-pick":      {"start", "continue", "abort", "skip", "quit", "apply"},
		"history.revert":           {"start", "continue", "abort", "skip", "quit", "apply"},
		"history.am":               {"start", "continue", "abort", "skip", "quit", "retry", "allow-empty"},
		"worktree.apply":           {"patch"},
		"notes":                    {"add", "append", "copy", "edit", "remove", "prune", "merge"},
		"history.bisect":           {"start", "good", "bad", "skip", "next", "reset", "run", "replay", "mark"},
		"worktree.sparse-checkout": {"init", "set", "add", "reapply", "disable", "clean"},
	} {
		for _, mode := range modes {
			Register(Spec{Family: family, Action: family + "." + mode, Effect: model.EffectWrite})
		}
	}
	// worktree.clean is a pre-existing public action, not worktree.clean.remove.
	Register(Spec{Family: "worktree", Action: "worktree.clean", Effect: model.EffectWrite})
	for family, modes := range map[string][]string{
		"worktree.clean": {"preview"}, "index.add": {"preview"}, "index.rm": {"preview"},
		"worktree.mv": {"preview"}, "stash": {"list", "show"}, "history.commit": {"preview"},
		"history.rebase": {"show-current-patch"}, "history.am": {"show-current-patch"},
		"worktree.apply": {"check", "stat"}, "notes": {"list", "show", "get-ref", "preview"},
		"history.bisect": {"log", "terms", "visualize"}, "worktree.sparse-checkout": {"list", "check-rules", "preview"},
	} {
		for _, mode := range modes {
			Register(Spec{Family: family, Action: family + "." + mode, Effect: model.EffectRead})
		}
	}
}
