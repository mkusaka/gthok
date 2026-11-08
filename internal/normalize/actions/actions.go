// Package actions is the shared curated Git family/action/effect catalogue.
package actions

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
)

type Spec struct {
	Family string
	Action string
	Effect model.Effect
}

var curated = func() map[string]Spec {
	out := make(map[string]Spec)
	for _, action := range []string{"remote.push.update", "remote.push.delete", "remote.push.mirror", "ref.branch.create", "ref.branch.delete", "ref.branch.list", "ref.update", "ref.delete", "worktree.reset.hard", "worktree.clean", "history.commit.create", "history.commit.amend", "config.set", "config.get", "status.show", "git.help.read", "git.version.read", "git.info.read", "unknown"} {
		family := action
		if i := strings.LastIndex(action, "."); i >= 0 {
			family = action[:i]
		}
		effect := model.EffectWrite
		if action == "ref.branch.list" || action == "config.get" || action == "status.show" || strings.HasPrefix(action, "git.") {
			effect = model.EffectRead
		}
		if action == "unknown" {
			effect = model.EffectUnknown
		}
		out[action] = Spec{family, action, effect}
	}
	return out
}()

// Register adds catalogue entries. Semantics files call it from init(); a
// duplicate action with a different identity panics at startup.
func Register(specs ...Spec) {
	for _, spec := range specs {
		if old, ok := curated[spec.Action]; ok && old != spec {
			panic("actions: conflicting registration for " + spec.Action)
		}
		curated[spec.Action] = spec
	}
}

func Must(action string) Spec {
	spec, ok := curated[action]
	if !ok {
		panic("uncurated action: " + action)
	}
	return spec
}
func Lookup(action string) (Spec, bool) { spec, ok := curated[action]; return spec, ok }
func All() []Spec {
	specs := make([]Spec, 0, len(curated))
	for _, spec := range curated {
		specs = append(specs, spec)
	}
	slices.SortFunc(specs, func(a, b Spec) int { return cmp.Compare(a.Action, b.Action) })
	return specs
}
func Apply(op *model.Operation, action string) {
	spec := Must(action)
	op.Family, op.Action, op.Effect = spec.Family, spec.Action, spec.Effect
}
func KnownPattern(glob string) bool {
	for action := range curated {
		if yes, _ := path.Match(glob, action); yes {
			return true
		}
	}
	return false
}
