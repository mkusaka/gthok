package actions

import "github.com/mkusaka/gthok/internal/model"

func init() {
	for family, modes := range map[string][]string{
		"ref.branch":      {"upstream", "description"},
		"ref":             {"create", "update", "delete", "rename", "pack"},
		"ref.reflog":      {"expire", "delete", "drop", "write"},
		"ref.replace":     {"create", "delete", "edit", "graft", "convert"},
		"ref.transaction": {"prepare", "commit"},
		"remote.push":     {"update", "delete", "mirror", "matching", "tags"},
		"remote.config":   {"add", "remove", "rename", "set-url", "set-branches"},
		"remote":          {"fetch", "pull", "prune", "clone"},
		"remote.pack":     {"receive", "fetch"},
	} {
		for _, mode := range modes {
			Register(Spec{Family: family, Action: family + "." + mode, Effect: model.EffectWrite})
		}
	}
	for family, modes := range map[string][]string{
		"ref.branch": {"list"}, "ref.tag": {"list", "verify"},
		"ref": {"list", "verify"}, "ref.symbolic": {"read", "verify"},
		"ref.reflog": {"show", "list", "exists"}, "ref.replace": {"list"},
		"ref.transaction": {"start", "abort", "option"},
		"remote":          {"list", "show", "get-url", "ls-remote"},
		"remote.pack":     {"upload", "advertise"},
	} {
		for _, mode := range modes {
			Register(Spec{Family: family, Action: family + "." + mode, Effect: model.EffectRead})
		}
	}
}
