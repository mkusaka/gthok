package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func init() {
	register("fetch", remoteFetch)
	register("pull", remotePull)
	register("remote", remoteCommand)
	register("ls-remote", remoteList)
	register("clone", remoteClone)
	register("send-pack", func(c *Context) []model.Operation { return rrPush(c, true) })
	register("receive-pack", remotePack)
	register("upload-pack", remotePack)
	register("fetch-pack", remotePack)
}

func rrOptionValues(p ParsedCommand, name string) []string {
	values := []string{}
	for _, o := range p.Options {
		if o.Name == name {
			if o.Negated {
				values = nil
			} else if o.HasValue {
				values = append(values, o.Value)
			}
		}
	}
	return values
}

func rrParameter(op *model.Operation, p ParsedCommand, name string) {
	o, ok := p.Option(name)
	if !ok {
		return
	}
	parts := strings.Split(name, "-")
	key := parts[0]
	for _, part := range parts[1:] {
		key += strings.ToUpper(part[:1]) + part[1:]
	}
	if o.Negated {
		op.Parameters[key] = false
	} else if o.HasValue {
		op.Parameters[key] = o.Value
	} else {
		op.Parameters[key] = true
	}
}

func rrExplicitExec(op *model.Operation, p ParsedCommand, names ...string) {
	var selected *ParsedOption
	for i := range p.Options {
		for _, name := range names {
			if p.Options[i].Name == name {
				selected = &p.Options[i]
			}
		}
	}
	if selected != nil && !selected.Negated {
		op.Capabilities = append(op.Capabilities, model.CapabilityExternalExec)
		op.Parameters["exec"] = selected.Value
	}
}

func remoteFetch(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	base := rrOperation("remote.fetch", model.ScopeLocalRef, model.ScopeObjects)
	base.Capabilities = []model.Capability{model.CapabilityNetwork}
	base.Target.Refs = model.UnknownSet()
	base.Parameters["dryRun"] = p.Has("dry-run")
	writeHead := !p.Has("dry-run") && !p.Has("negotiate-only")
	if o, ok := p.Option("write-fetch-head"); ok && o.Negated {
		writeHead = false
	}
	base.Parameters["writeFetchHead"] = writeHead
	if writeHead {
		base.Parameters["fetchHead"] = "FETCH_HEAD"
	}
	for _, name := range []string{"all", "multiple", "prune", "prune-tags", "append", "force", "atomic", "depth", "deepen", "unshallow", "shallow-since", "update-shallow", "set-upstream", "prefetch", "recurse-submodules", "negotiate-only", "tags"} {
		rrParameter(&base, p, name)
	}
	for _, o := range p.Options {
		switch o.Name {
		case "tags":
			base.Parameters["tags"] = !o.Negated
		case "n":
			base.Parameters["tags"] = false
		}
	}
	for _, flag := range []string{"atomic", "append", "force"} {
		switchEdit(c, &base, flag, flag)
	}
	base.Parameters["refmap"], base.Parameters["serverOptions"] = rrOptionValues(p, "refmap"), rrOptionValues(p, "server-option")
	base.Parameters["shallowExclude"] = rrOptionValues(p, "shallow-exclude")
	rrExplicitExec(&base, p, "upload-pack")
	if p.Has("set-upstream") && !p.Has("dry-run") {
		base.Scopes = append(base.Scopes, model.ScopeConfig)
	}
	if o, ok := p.Option("recurse-submodules"); ok && !o.Negated && o.Value != "no" && o.Value != "false" {
		base.Scopes = append(base.Scopes, model.ScopeSubmodule)
	}
	if p.Has("dry-run") || p.Has("negotiate-only") {
		base.Effect = model.EffectRead
	}
	remotes, refspecs := []string{""}, []string{}
	switch {
	case p.Has("all"):
		if len(args) > 0 {
			return rrInvalid(base, "fetch --all does not take a repository or refspecs")
		}
	case p.Has("multiple"):
		if len(args) > 0 {
			remotes = args
		}
	case len(args) > 0:
		remotes, refspecs = args[:1], args[1:]
	}
	if p.Has("stdin") {
		if c.Invocation.Stdin.Status != model.StdinCaptured {
			base.Coverage, base.UnknownReason, base.Effect = model.CoverageOpaque, model.UnknownInputUnavailable, model.EffectUnknown
			rrUnresolved(&base, "input", "fetch refspec input was not captured", "stdin")
		} else {
			refspecs = append(append([]string(nil), refspecs...), strings.Fields(string(c.Invocation.Stdin.Data))...)
		}
	}
	ops := make([]model.Operation, 0, len(remotes))
	for _, remote := range remotes {
		op := base
		op.Parameters = rrCopyParameters(base.Parameters)
		op.Unresolved = append([]model.Unresolved(nil), base.Unresolved...)
		op.Target.Remote = remote
		op.Parameters["refspecs"] = refspecs
		rrRemoteResolution(&op, true)
		local, source := []string{}, []string{}
		for i := 0; i < len(refspecs); i++ {
			refspec := strings.TrimPrefix(refspecs[i], "+")
			if strings.HasPrefix(refspec, "^") {
				continue
			}
			if refspec == "tag" && i+1 < len(refspecs) {
				i++
				refspec = "refs/tags/" + refspecs[i] + ":refs/tags/" + refspecs[i]
			}
			src, dst, colon := strings.Cut(refspec, ":")
			source = append(source, src)
			if colon && dst != "" {
				local = append(local, dst)
			}
		}
		if writeHead {
			local = append(local, "FETCH_HEAD")
		}
		op.Target.Refs = rrSet(local, strings.Contains(strings.Join(local, ""), "*"))
		op.Target.Refs.Complete = false
		op.Target.Revisions = rrSet(source, strings.Contains(strings.Join(source, ""), "*"))
		rrUnresolved(&op, "target.refs", "fetch refmaps, tags and pruning require configuration and advertised remote refs", "git")
		if len(remotes) == 1 && len(p.Positionals) > 0 {
			rrEditable(c, &op, "/target/remote", p.Positionals[0])
			if len(refspecs) == 1 && len(p.Positionals) == 2 && !p.Has("stdin") {
				rrRefspecEdit(c, &op, p.Positionals[1])
			}
		}
		ops = append(ops, op)
	}
	return ops
}

func remotePull(c *Context) []model.Operation {
	ops := remoteFetch(c)
	integrate := rrOperation("remote.pull", model.ScopeLocalRef, model.ScopeIndex, model.ScopeWorktree, model.ScopeObjects, model.ScopeReflog)
	integrate.Origin.Mode = "integrate"
	integrate.Target.Refs = model.UnknownSet()
	integrate.Target.Revisions = model.Exact("FETCH_HEAD")
	rrUnresolved(&integrate, "target.refs", "pull integrates into the current branch", "git")
	integrate.Parameters["mode"] = "configured"
	if o, ok := c.Parsed.Option("rebase"); ok {
		switch {
		case o.Negated || o.Value == "false":
			integrate.Parameters["mode"] = "merge"
		case o.Value == "interactive" || o.Value == "i":
			integrate.Parameters["mode"] = "rebase"
			integrate.Effect, integrate.Coverage, integrate.UnknownReason = model.EffectUnknown, model.CoverageOpaque, model.UnknownInteractive
			integrate.Capabilities = append(integrate.Capabilities, model.CapabilityInteractive, model.CapabilityEditor, model.CapabilityExternalExec)
			rrUnresolved(&integrate, "parameters.todo", "interactive rebase todo may include exec commands", "editor")
		default:
			integrate.Parameters["mode"] = "rebase"
		}
	} else {
		rrUnresolved(&integrate, "parameters.mode", "pull.rebase/branch rebase and pull.ff require configuration", "git")
	}
	if c.Parsed.Has("squash") {
		integrate.Parameters["mode"] = "merge"
		integrate.Parameters["squash"] = true
	}
	for _, name := range []string{"ff-only", "ff", "autostash", "commit", "edit", "recurse-submodules"} {
		rrParameter(&integrate, c.Parsed, name)
	}
	if !c.Parsed.Has("dry-run") {
		if e, ok := c.Parsed.Option("edit"); !ok || !e.Negated {
			integrate.Capabilities = append(integrate.Capabilities, model.CapabilityEditor)
		}
		if c.Parsed.Has("autostash") {
			integrate.Scopes = append(integrate.Scopes, model.ScopeStash)
		}
	} else {
		integrate.Effect, integrate.Coverage, integrate.UnknownReason = model.EffectRead, model.CoverageSemantic, model.UnknownNone
		integrate.Execution, integrate.Evidence = model.ExecutionSkipped, "pull --dry-run returns immediately after fetch"
		integrate.Capabilities = nil
	}
	return append(ops, integrate)
}

func remoteCommand(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	mode := "list"
	if len(p.Path) > 1 {
		mode = p.Path[1]
	}
	action := "remote." + mode
	switch mode {
	case "add", "rename", "remove", "set-url", "set-branches":
		action = "remote.config." + mode
	case "rm":
		action = "remote.config.remove"
	case "update":
		fc := *c
		fc.Parsed.Options = append(append([]ParsedOption(nil), p.Options...), ParsedOption{Name: "multiple"})
		ops := remoteFetch(&fc)
		for i := range ops {
			ops[i].Parameters["remoteUpdate"] = true
			if len(args) == 0 {
				rrUnresolved(&ops[i], "target.remote", "remote update defaults to remotes.default or all eligible remotes", "git")
			}
		}
		return ops
	case "set-head":
		action = "ref.symbolic.set"
		if p.Has("delete") {
			action = "ref.symbolic.delete"
		}
	}
	op := rrOperation(action, model.ScopeRemoteConfig)
	if len(args) > 0 {
		op.Target.Remote = args[0]
		rrEditable(c, &op, "/target/remote", p.Positionals[0])
	}
	if strings.HasPrefix(action, "remote.config.") {
		op.Scopes = append(op.Scopes, model.ScopeConfig)
		if len(args) == 0 {
			return rrInvalid(op, "remote name required")
		}
		op.Target.ConfigKeys = rrSet([]string{"remote." + args[0] + ".*"}, true)
	}
	for _, name := range []string{"push", "add", "delete", "all", "tags", "mirror", "auto", "dry-run"} {
		rrParameter(&op, p, name)
	}
	switch mode {
	case "list":
		return []model.Operation{op}
	case "add":
		if len(args) != 2 {
			return rrInvalid(op, "remote add expects a name and URL")
		}
		op.Parameters["url"] = args[1]
		op.Parameters["track"] = rrOptionValues(p, "track")
		if o, ok := p.Option("master"); ok && !o.Negated {
			op.Scopes = append(op.Scopes, model.ScopeLocalRef)
			op.Target.Refs = model.Exact("refs/remotes/" + args[0] + "/HEAD")
			op.Parameters["head"] = o.Value
		}
		if p.Has("fetch") {
			fc := *c
			fc.Parsed = ParsedCommand{Path: []string{"fetch"}, Positionals: []ParsedArgument{p.Positionals[0]}}
			return append([]model.Operation{op}, remoteFetch(&fc)...)
		}
	case "rename":
		if len(args) != 2 {
			return rrInvalid(op, "remote rename expects old and new names")
		}
		op.Parameters["newName"] = args[1]
		op.Target.Refs = rrSet([]string{"refs/remotes/" + args[0] + "/*", "refs/remotes/" + args[1] + "/*"}, true)
		op.Scopes = append(op.Scopes, model.ScopeLocalRef, model.ScopeReflog)
	case "remove", "rm":
		op.Target.Refs = model.UnknownSet()
		op.Scopes = append(op.Scopes, model.ScopeLocalRef, model.ScopeReflog)
		rrUnresolved(&op, "target.refs", "removed remote tracking refs depend on configured fetch refmaps", "git")
	case "set-url":
		if len(args) < 2 || len(args) > 3 {
			return rrInvalid(op, "remote set-url expects name and URL, with optional old URL pattern")
		}
		op.Parameters["url"] = args[1]
		if len(args) > 2 {
			op.Parameters["oldURL"] = args[2]
		}
	case "set-branches":
		op.Parameters["branches"] = args[1:]
	case "set-head":
		if len(args) < 1 {
			return rrInvalid(op, "remote set-head requires a remote")
		}
		op.Scopes = []model.Scope{model.ScopeLocalRef}
		op.Target.Refs = model.Exact("refs/remotes/" + args[0] + "/HEAD")
		if len(args) > 1 {
			op.Parameters["newTarget"] = "refs/remotes/" + args[0] + "/" + args[1]
		}
		if p.Has("auto") {
			op.Capabilities = []model.Capability{model.CapabilityNetwork}
			rrUnresolved(&op, "parameters.newTarget", "default branch is advertised by the remote", "remote")
		}
	case "prune", "show":
		if mode == "prune" || !p.Has("n") {
			op.Capabilities = []model.Capability{model.CapabilityNetwork}
		}
		if mode == "prune" {
			op.Scopes = []model.Scope{model.ScopeLocalRef, model.ScopeReflog}
			op.Target.Refs = model.UnknownSet()
			op.Parameters["dryRun"] = p.Has("dry-run")
			rrUnresolved(&op, "target.refs", "stale tracking refs depend on remote advertisements and fetch refmaps", "remote")
			if p.Has("dry-run") {
				op.Effect = model.EffectRead
			}
		}
		if len(args) > 1 {
			ops := make([]model.Operation, len(args))
			for i, remote := range args {
				ops[i] = op
				ops[i].Target.Remote = remote
				ops[i].Editable = nil
			}
			return ops
		}
	case "get-url":
		rrUnresolved(&op, "target.remoteUrl", "URL rewrites require local configuration", "git")
	}
	return []model.Operation{op}
}

func remoteList(c *Context) []model.Operation {
	op := rrOperation("remote.ls-remote", model.ScopeRemoteRef)
	args := c.Parsed.Values()
	if len(args) > 0 {
		op.Target.Remote, args = args[0], args[1:]
		rrEditable(c, &op, "/target/remote", c.Parsed.Positionals[0])
	}
	rrRemoteResolution(&op, false)
	if c.Parsed.Has("get-url") {
		actions.Apply(&op, "remote.get-url")
		op.Scopes = []model.Scope{model.ScopeRemoteConfig}
	} else {
		op.Capabilities = []model.Capability{model.CapabilityNetwork}
		rrExplicitExec(&op, c.Parsed, "upload-pack", "exec")
		if len(args) == 0 {
			args = []string{"*"}
		}
		op.Target.Refs = rrSet(args, true)
	}
	return []model.Operation{op}
}

func remoteClone(c *Context) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	op := rrOperation("remote.clone", model.ScopeObjects, model.ScopeLocalRef, model.ScopeConfig)
	op.Capabilities = []model.Capability{model.CapabilityNetwork}
	if len(args) < 1 || len(args) > 2 {
		return rrInvalid(op, "clone expects a repository and optional directory")
	}
	op.Target.Remote = args[0]
	bare := false
	for _, o := range p.Options {
		if o.Name == "bare" || o.Name == "naked" {
			bare = !o.Negated
		}
	}
	bare = bare || p.Has("mirror")
	checkout := !bare && !p.Has("no-checkout")
	op.Parameters["bare"], op.Parameters["checkout"], op.Parameters["mirror"] = bare, checkout, p.Has("mirror")
	if !bare {
		op.Scopes = append(op.Scopes, model.ScopeWorktree)
		if checkout {
			op.Scopes = append(op.Scopes, model.ScopeIndex)
		}
	}
	if len(args) == 2 {
		op.Target.Paths = model.Exact(args[1])
	} else {
		op.Target.Paths = model.UnknownSet()
		rrUnresolved(&op, "target.paths", "clone directory is derived from the repository transport path", "git")
	}
	for _, name := range []string{"depth", "unshallow", "single-branch", "branch", "revision", "origin", "separate-git-dir", "recurse-submodules", "local", "shared"} {
		rrParameter(&op, p, name)
	}
	if o, ok := p.Option("recurse-submodules"); ok && !o.Negated && checkout {
		op.Scopes = append(op.Scopes, model.ScopeSubmodule)
	}
	rrExplicitExec(&op, p, "upload-pack")
	return []model.Operation{op}
}

func remotePack(c *Context) []model.Operation {
	cmd := c.Parsed.Path[0]
	action := "remote.pack.fetch"
	scopes := []model.Scope{model.ScopeObjects}
	switch cmd {
	case "receive-pack":
		action = "remote.pack.receive"
		scopes = append(scopes, model.ScopeLocalRef, model.ScopeReflog, model.ScopeWorktree, model.ScopeIndex)
	case "upload-pack":
		action = "remote.pack.upload"
	}
	if c.Parsed.Has("http-backend-info-refs") {
		action = "remote.pack.advertise"
	}
	op := rrOperation(action, scopes...)
	op.Capabilities = []model.Capability{model.CapabilityNetwork}
	args := c.Parsed.Values()
	if len(args) == 0 {
		return rrInvalid(op, "pack transport requires a repository")
	}
	if cmd == "fetch-pack" {
		op.Target.Remote = args[0]
		op.Target.Revisions = rrSet(args[1:], false)
	} else {
		op.Target.Paths = model.Exact(args[0])
	}
	if cmd == "receive-pack" && !c.Parsed.Has("http-backend-info-refs") {
		op.Target.Refs = model.UnknownSet()
		op.Coverage = model.CoverageOpaque
		op.Effect, op.UnknownReason = model.EffectUnknown, model.UnknownInputUnavailable
		rrUnresolved(&op, "input", "receive-pack commands arrive through a bidirectional protocol", "stdin")
	}
	return []model.Operation{op}
}
