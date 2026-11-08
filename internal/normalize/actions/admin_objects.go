package actions

import "github.com/mkusaka/gthok/internal/model"

func init() {
	for family, names := range map[string][]string{
		"config":         {"set", "add", "unset", "rename-section", "remove-section", "edit"},
		"worktree.admin": {"add", "lock", "unlock", "move", "prune", "remove", "repair"},
		"submodule":      {"add", "clone", "init", "deinit", "update", "sync", "set-url", "set-branch", "absorbgitdirs"},
		"repository":     {"init"},
		"objects":        {"hash.write", "write-tree", "commit-tree", "mktag", "mktree", "unpack-objects", "index-pack", "pack-objects", "prune", "prune-packed", "repack", "gc", "fsck.lost-found", "fast-export.output", "commit-graph.write", "multi-pack-index.write", "multi-pack-index.repack", "multi-pack-index.compact", "multi-pack-index.expire"},
		"index":          {"read-tree", "update"},
		"maintenance":    {"run", "start", "stop", "register", "unregister"},
		"archive":        {"output"},
		"bundle":         {"create", "unbundle"},
	} {
		for _, name := range names {
			Register(Spec{Family: family, Action: family + "." + name, Effect: model.EffectWrite})
		}
	}
	for family, names := range map[string][]string{
		"config":         {"get", "list"},
		"worktree.admin": {"list", "prune.preview"},
		"submodule":      {"status", "summary", "gitdir", "get-default-remote"},
		"objects":        {"hash.read", "fast-export", "unpack-objects.preview", "index-pack.verify", "pack-objects.read", "prune.preview", "prune-packed.preview", "repack.preview", "count-objects", "fsck", "verify-pack", "commit-graph.verify", "multi-pack-index.verify"},
		"index":          {"read-tree.preview"},
		"maintenance":    {"is-needed"},
		"hook":           {"list"},
		"archive":        {"create", "list"},
		"bundle":         {"verify", "list-heads", "stdout"},
	} {
		for _, name := range names {
			Register(Spec{Family: family, Action: family + "." + name, Effect: model.EffectRead})
		}
	}
	for _, command := range []string{
		"log", "show", "diff", "diff-tree", "diff-index", "diff-files", "diff-pairs", "rev-list", "rev-parse", "cat-file", "ls-files", "ls-tree", "grep", "blame", "annotate", "pickaxe", "shortlog", "describe", "name-rev", "format-rev", "merge-base", "show-branch", "whatchanged", "range-diff", "format-patch", "last-modified", "cherry", "check-attr", "check-ignore", "check-mailmap", "check-ref-format", "var", "repo", "column", "stripspace", "patch-id", "show-index", "get-tar-commit-id", "fmt-merge-msg", "pack-redundant", "url-parse", "verify-commit", "verify-tag",
	} {
		family := "git." + command
		Register(Spec{Family: family, Action: family + ".read", Effect: model.EffectRead})
	}
	for _, command := range []string{"log", "show", "diff", "diff-tree", "diff-index", "diff-files", "diff-pairs", "rev-list", "rev-parse", "blame", "annotate", "pickaxe", "shortlog", "format-rev", "whatchanged", "range-diff", "format-patch", "last-modified"} {
		family := "git." + command
		Register(Spec{Family: family, Action: family + ".output", Effect: model.EffectWrite})
	}
	Register(
		Spec{Family: "objects", Action: "objects.fast-import", Effect: model.EffectUnknown},
		Spec{Family: "submodule", Action: "submodule.foreach", Effect: model.EffectUnknown},
		Spec{Family: "hook", Action: "hook.run", Effect: model.EffectUnknown},
		Spec{Family: "git.cat-file", Action: "git.cat-file.protocol", Effect: model.EffectUnknown},
	)
}
