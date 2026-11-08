package resolve

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	gitnormalize "github.com/mkusaka/gthok/internal/normalize/git"
)

func (r *Resolver) loadConfig(ctx context.Context) {
	data, err := r.run(ctx, query{kind: configQuery})
	if err != nil {
		r.local.Unresolved = append(r.local.Unresolved, model.Unresolved{Field: "remotes", Reason: err.Error(), Source: "git"})
		return
	}
	r.config = make(map[string][]string)
	for _, entry := range strings.Split(string(data), "\x00") {
		if entry == "" {
			continue
		}
		key, value, hasValue := strings.Cut(entry, "\n")
		if !hasValue {
			value = "true"
		}
		if _, exists := r.config[key]; !exists {
			r.configOrder = append(r.configOrder, key)
		}
		r.config[key] = append(r.config[key], value)
	}
}

func (r *Resolver) boolConfig(ctx context.Context, key string, fallback bool) (bool, bool) {
	if r.config == nil {
		return false, false
	}
	if _, exists := r.config[key]; !exists {
		return fallback, true
	}
	data, err := r.run(ctx, query{kind: configBoolQuery, value: key})
	value := strings.TrimSuffix(string(data), "\x00")
	return value == "true", err == nil && (value == "true" || value == "false")
}

func addCapability(op *model.Operation, capability model.Capability) {
	if !slices.Contains(op.Capabilities, capability) {
		op.Capabilities = append(op.Capabilities, capability)
	}
}

func addScope(op *model.Operation, scope model.Scope) {
	if !slices.Contains(op.Scopes, scope) {
		op.Scopes = append(op.Scopes, scope)
	}
}

// Consume markers individually: two entries for one field may describe
// independent sources (e.g. pathspec expansion and submodule side effects).
func (r *Resolver) semantic(ctx context.Context, op *model.Operation, parsed gitnormalize.ParsedCommand) {
	pending := op.Unresolved
	op.Unresolved = nil
	for _, u := range pending {
		if u.Source != "git" || !r.semanticEntry(ctx, op, u, parsed) {
			op.Unresolved = append(op.Unresolved, u)
		}
	}
	if op.Target.Refs != nil && hasUnresolved(*op, "target.refs") {
		op.Target.Refs.Complete = false
	}
	if op.Target.Paths != nil && hasUnresolved(*op, "target.paths") {
		op.Target.Paths.Complete = false
	}
}

func (r *Resolver) semanticEntry(ctx context.Context, op *model.Operation, u model.Unresolved, p gitnormalize.ParsedCommand) bool {
	switch u.Field {
	case "parameters.requireForce", "parameters.recurseSubmodules", "parameters.updateRefs":
		key, parameter, fallback := "clean.requireforce", "requireForce", true
		switch u.Field {
		case "parameters.recurseSubmodules":
			key, parameter, fallback = "submodule.recurse", "recurseSubmodules", false
		case "parameters.updateRefs":
			key, parameter, fallback = "rebase.updaterefs", "updateRefs", false
		}
		value, ok := r.boolConfig(ctx, key, fallback)
		if !ok {
			return false
		}
		op.Parameters[parameter] = value
		if value && parameter == "recurseSubmodules" {
			addScope(op, model.ScopeSubmodule)
		}
		if value && parameter == "updateRefs" {
			op.Target.Refs = model.UnknownSet()
			op.Unresolved = append(op.Unresolved, model.Unresolved{Field: "target.refs", Reason: "other branches pointing at rebased commits are updated, excluding checked-out branches", Source: "git"})
		}
		// requireForce never changes a deletion into a read; false permits it.
		return true
	case "parameters.signed":
		signed, ok := r.boolConfig(ctx, "tag.gpgsign", false)
		force, forceOK := r.boolConfig(ctx, "tag.forcesignannotated", false)
		if !ok || !forceOK {
			return false
		}
		if force && flag(*op, "annotated") {
			if len(p.Path) == 0 {
				return false // The semantic parameters do not retain explicit -a.
			}
			signed = signed || !p.Has("annotate")
		}
		op.Parameters["signed"] = signed
		if signed {
			op.Parameters["annotated"] = true
			addCapability(op, model.CapabilityExternalExec)
			addScope(op, model.ScopeObjects)
			if op.Texts == nil {
				addCapability(op, model.CapabilityEditor)
				op.Unresolved = append(op.Unresolved, model.Unresolved{Field: "texts.body", Reason: "signed tag message will be edited", Source: "editor"})
			}
		}
		return true
	case "parameters.externalDiff":
		if r.config == nil {
			return false
		}
		external := r.env["GIT_EXTERNAL_DIFF"] != "" || r.last("diff.external") != ""
		op.Parameters["externalDiff"] = external
		if external {
			addCapability(op, model.CapabilityExternalExec)
		}
		return !r.hasDriver("diff.", ".command")
	case "parameters.filters", "capabilities":
		if u.Field == "capabilities" && u.Reason != "attributes may select external clean/smudge filters" {
			return false
		}
		if r.config == nil {
			return false
		}
		// Attribute selection is deliberately not guessed from filenames.
		if r.hasDriver("filter.", ".clean", ".smudge", ".process") || r.hasDriver("diff.", ".textconv") {
			return false
		}
		op.Parameters["filters"] = false
		return true
	case "parameters.mode":
		if op.Family == "remote.pull" || op.Action == "remote.pull" {
			return r.pullMode(ctx, op)
		}
	case "parameters.tasks":
		return r.maintenanceTasks(ctx, op, p)
	case "parameters.createBranch":
		return r.branchCreation(ctx, op, p)
	case "parameters.archiver":
		format, ok := p.Option("format")
		if r.config == nil || !ok {
			return false
		}
		op.Parameters["archiver"] = r.last("tar."+format.Value+".command") != ""
		return true
	case "parameters.updateCommand":
		if r.config == nil {
			return false
		}
		external := false
		for key := range r.config {
			if strings.HasPrefix(key, "submodule.") && strings.HasSuffix(key, ".update") && strings.HasPrefix(r.last(key), "!") {
				external = true
			}
		}
		op.Parameters["updateCommand"] = external
		// .gitmodules rejects !commands; local commands still need path selection.
		return !external
	case "target.configKeys":
		return r.configKeys(ctx, op, p)
	case "target.paths":
		if u.Reason == "expand pathspecs relative to the effective repository and prefix" || u.Reason == "affected paths depend on repository state" {
			return r.pathspecs(ctx, op, p)
		}
	case "target.refs":
		if u.Reason == "" && targetKnown(op.Target, "target.refs") {
			return true
		}
		switch u.Reason {
		case "update-ref follows symbolic references unless --no-deref":
			return r.dereference(ctx, op)
		case "reflog names require local ref resolution":
			if op.Target.Refs == nil || op.Target.Refs.Kind == model.SetUnknown {
				return false
			}
			var names []string
			for _, name := range op.Target.Refs.Values {
				if name != "HEAD" && !strings.HasPrefix(name, "refs/") {
					full, err := r.dwim(ctx, name)
					if err != nil {
						return false
					}
					name = full
				}
				names = append(names, name)
			}
			setRefs(op, names)
			return true
		case "export ref requires full refname resolution":
			name, _ := op.Parameters["toRef"].(string)
			if !strings.HasPrefix(name, "refs/") {
				return false
			}
			setRefs(op, []string{name})
			return true
		case "HEAD may be symbolic or detached", "current branch requires symbolic HEAD resolution", "pull integrates into the current branch":
			if r.local.GitDir == "" {
				return false
			}
			ref := r.local.Branch
			if ref == "" {
				if r.local.HeadSHA == "" || strings.HasPrefix(op.Action, "ref.branch.") {
					return false
				}
				ref = "HEAD"
			}
			names := []string{ref}
			if op.Target.Refs != nil {
				names = append(names, op.Target.Refs.Values...)
			}
			setRefs(op, names)
			if op.Action == "ref.branch.upstream" || op.Action == "ref.branch.description" {
				op.Target.ConfigKeys = &model.TargetSet{Kind: model.SetPattern, Values: []string{"branch." + strings.TrimPrefix(ref, "refs/heads/") + ".*"}, Complete: true}
			}
			return true
		case "core.notesRef defaults to refs/notes/commits":
			if r.config == nil {
				return false
			}
			ref, exists := r.env["GIT_NOTES_REF"]
			if !exists {
				ref = "refs/notes/commits"
				if _, configured := r.config["core.notesref"]; configured {
					ref = r.last("core.notesref")
				}
			}
			if !strings.HasPrefix(ref, "refs/") {
				return false
			}
			setRefs(op, []string{ref})
			return true
		case "initial branch name depends on init.defaultBranch":
			if r.config == nil {
				return false
			}
			branch := r.env["GIT_TEST_DEFAULT_INITIAL_BRANCH_NAME"]
			if branch == "" {
				branch = r.last("init.defaultbranch")
			}
			if branch == "" {
				branch = "master" // Git v2.56's compiled default.
			}
			op.Parameters["initialBranch"] = branch
			setRefs(op, []string{"refs/heads/" + branch})
			return true
		}
	case "target.revisions":
		if u.Reason == "upstream or working branch defaults require repository state" && r.local.GitDir != "" {
			values := append([]string(nil), p.Values()...)
			if len(values) == 0 && !flag(*op, "root") {
				values = r.upstreams(ctx)
				if len(values) != 1 {
					return false
				}
			}
			branch := r.local.Branch
			if branch == "" {
				if r.local.HeadSHA == "" {
					return false
				}
				branch = "HEAD"
			}
			op.Target.Revisions = targetSet(append(values, branch))
			return true
		}
		if u.Reason == "default merge heads come from branch upstream configuration" {
			if upstream := r.upstreams(ctx); len(upstream) > 0 {
				op.Target.Revisions = targetSet(upstream)
				return true
			}
		}
	}
	return false
}

func (r *Resolver) hasDriver(prefix string, suffixes ...string) bool {
	for key, values := range r.config {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		for _, suffix := range suffixes {
			if strings.HasSuffix(key, suffix) && len(values) > 0 && values[len(values)-1] != "" {
				return true
			}
		}
	}
	return false
}

func targetSet(values []string) *model.TargetSet {
	kind := model.SetSet
	if len(values) == 0 {
		kind = model.SetNone
	} else if len(values) == 1 {
		kind = model.SetExact
	}
	return &model.TargetSet{Kind: kind, Values: values, Complete: true}
}

func (r *Resolver) configKeys(ctx context.Context, op *model.Operation, p gitnormalize.ParsedCommand) bool {
	pattern, _ := op.Parameters["keyPattern"].(string)
	if pattern == "" || op.Parameters["location"] != "all" || (op.Origin.Mode != "get-regexp" && !p.Has("regexp")) || p.Has("url") || op.Parameters["valuePattern"] != nil {
		return false
	}
	data, err := r.run(ctx, query{kind: configRegexpQuery, value: pattern})
	if err != nil && !exitCode(err, 1) {
		return false
	}
	var keys []string
	for _, record := range strings.Split(string(data), "\x00") {
		key, _, _ := strings.Cut(record, "\n")
		if key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	op.Target.ConfigKeys = targetSet(keys)
	return true
}

func (r *Resolver) upstreams(ctx context.Context) []string {
	branch := strings.TrimPrefix(r.local.Branch, "refs/heads/")
	remote := r.last("branch." + branch + ".remote")
	merges := r.config["branch."+branch+".merge"]
	if remote == "" || len(merges) == 0 {
		return nil
	}
	var refs []string
	for _, merge := range merges {
		mapped := ""
		if remote == "." {
			mapped = merge
		} else {
			for _, spec := range r.config["remote."+remote+".fetch"] {
				source, dest, colon := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
				if !colon || strings.HasPrefix(source, "^") {
					continue
				}
				if source == merge {
					mapped = dest
				} else if strings.Count(source, "*") == 1 && strings.Count(dest, "*") == 1 {
					prefix, suffix, _ := strings.Cut(source, "*")
					if len(merge) >= len(prefix)+len(suffix) && strings.HasPrefix(merge, prefix) && strings.HasSuffix(merge, suffix) {
						mapped = strings.Replace(dest, "*", merge[len(prefix):len(merge)-len(suffix)], 1)
					}
				}
			}
		}
		if !strings.HasPrefix(mapped, "refs/") {
			return nil
		}
		if _, err := r.run(ctx, query{kind: commitQuery, value: mapped}); err != nil {
			return nil
		}
		refs = append(refs, mapped)
	}
	return refs
}

func (r *Resolver) pullMode(ctx context.Context, op *model.Operation) bool {
	if r.config == nil {
		return false
	}
	branch := strings.TrimPrefix(r.local.Branch, "refs/heads/")
	key := "branch." + branch + ".rebase"
	if _, exists := r.config[key]; !exists {
		key = "pull.rebase"
	}
	value := strings.ToLower(r.last(key))
	if flag(*op, "squash") {
		value = "" // An explicit squash uses merge, not configured interactive rebase.
	}
	mode := "merge"
	switch value {
	case "interactive", "i":
		mode = "rebase"
		op.Coverage, op.UnknownReason, op.Effect = model.CoverageOpaque, model.UnknownInteractive, model.EffectUnknown
		addCapability(op, model.CapabilityInteractive)
		addCapability(op, model.CapabilityEditor)
		addCapability(op, model.CapabilityExternalExec)
		op.Unresolved = append(op.Unresolved, model.Unresolved{Field: "parameters.todo", Reason: "interactive rebase todo may include exec commands", Source: "editor"})
	case "merges", "m", "preserve", "p":
		mode = "rebase"
	case "":
	default:
		rebase, ok := r.boolConfig(ctx, key, false)
		if !ok {
			return false
		}
		if rebase {
			mode = "rebase"
		}
	}
	if ff := r.last("pull.ff"); ff != "" {
		if ff == "only" {
			if _, explicit := op.Parameters["ffOnly"]; !explicit {
				op.Parameters["ffOnly"] = true
			}
		} else if value, ok := r.boolConfig(ctx, "pull.ff", true); ok {
			if _, explicit := op.Parameters["ff"]; !explicit {
				op.Parameters["ff"] = value
			}
		} else {
			return false
		}
	}
	if !flag(*op, "squash") {
		op.Parameters["mode"] = mode
	}
	return true
}
