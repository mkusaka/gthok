package resolve

import (
	"context"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
)

func (r *Resolver) dereference(ctx context.Context, op *model.Operation) bool {
	refs := op.Target.Refs
	if r.local.GitDir == "" || refs == nil || (refs.Kind != model.SetExact && refs.Kind != model.SetSet) {
		return false
	}
	var names []string
	for _, name := range refs.Values {
		if name != "HEAD" && !strings.HasPrefix(name, "refs/") {
			return false
		}
		data, err := r.run(ctx, query{kind: symbolicRefQuery, value: name})
		switch {
		case err == nil:
			name = strings.TrimSuffix(string(data), "\n")
			if !strings.HasPrefix(name, "refs/") || strings.ContainsAny(name, "\r\n") {
				return false
			}
		case !exitCode(err, 1):
			return false
		}
		names = append(names, name)
	}
	setRefs(op, names)
	return true
}

func (r *Resolver) branchCreation(ctx context.Context, op *model.Operation, p gitnormalize.ParsedCommand) bool {
	if r.config == nil || r.local.GitDir == "" || r.loadRefs(ctx) != nil {
		return false
	}
	worktree := op.Action == "worktree.admin.add"
	key := "checkout.guess"
	if worktree {
		key = "worktree.guessremote"
	}
	guess, ok := r.boolConfig(ctx, key, !worktree)
	if !ok {
		return false
	}
	if option, explicit := p.Option("guess"); explicit && !worktree {
		guess = !option.Negated
	}
	name := ""
	if worktree && op.Target.Refs != nil && len(op.Target.Refs.Values) == 1 {
		name = strings.TrimPrefix(op.Target.Refs.Values[0], "refs/heads/")
	} else if op.Target.Revisions != nil && len(op.Target.Revisions.Values) == 1 {
		name = op.Target.Revisions.Values[0]
	}
	if name == "" {
		return false
	}
	ref := "refs/heads/" + name
	if r.refs[ref] || name == "HEAD" {
		op.Parameters["createBranch"] = false
		return true
	}
	if worktree {
		op.Parameters["guessRemote"] = guess
		op.Parameters["createBranch"] = true
		return true // A missing basename branch is created, even without remote guessing.
	}
	if !guess {
		op.Parameters["createBranch"] = false
		return true
	}
	if _, err := r.run(ctx, query{kind: commitQuery, value: name}); err == nil {
		op.Parameters["createBranch"] = false
		return true
	}
	var remotes []string
	for remote := range r.local.Remotes {
		for _, spec := range r.config["remote."+remote+".fetch"] {
			source, dest, colon := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
			if !colon {
				continue
			}
			mapped := dest
			if source != ref {
				prefix, suffix, wildcard := strings.Cut(source, "*")
				if !wildcard || strings.Count(dest, "*") != 1 || len(ref) < len(prefix)+len(suffix) || !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, suffix) {
					continue
				}
				mapped = strings.Replace(dest, "*", ref[len(prefix):len(ref)-len(suffix)], 1)
			}
			if r.refs[mapped] {
				remotes = append(remotes, remote)
				break
			}
		}
	}
	selected := len(remotes) == 1
	for _, remote := range remotes {
		selected = selected || remote == r.last("checkout.defaultremote")
	}
	if !selected && len(remotes) > 1 {
		return false
	}
	op.Parameters["createBranch"] = selected
	if selected {
		op.Parameters["newBranch"] = name
		names := []string{ref}
		if op.Target.Refs != nil {
			names = append(names, op.Target.Refs.Values...)
		}
		setRefs(op, names)
	}
	return true
}
