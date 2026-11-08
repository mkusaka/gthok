package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/model"
	"github.com/mkusaka/gthok/internal/normalize/actions"
)

func init() { register("push", push) }

func push(c *Context) []model.Operation { return rrPush(c, false) }

func rrPush(c *Context, plumbing bool) []model.Operation {
	p, args := c.Parsed, c.Parsed.Values()
	base := rrOperation("remote.push.update", model.ScopeRemoteRef)
	base.Capabilities = []model.Capability{model.CapabilityNetwork}
	base.Parameters["forceMode"], base.Parameters["leaseMode"] = "none", "none"
	base.Parameters["dryRun"] = p.Has("dry-run")
	var remoteArg *ParsedArgument
	if len(args) > 0 {
		base.Target.Remote, args = args[0], args[1:]
		remoteArg = &p.Positionals[0]
	} else if repo, ok := p.Option("repo"); ok && !repo.Negated {
		base.Target.Remote = repo.Value
		if strings.Join(c.Argv, "\x00") == strings.Join(c.Invocation.Argv, "\x00") {
			base.Editable = map[string]model.EditLocator{"target/remote": {Kind: "argv", Flag: "repo", ValueType: "string", Spans: []model.ArgSpan{repo.Span}}}
		}
	}
	if remoteArg != nil {
		rrEditable(c, &base, "/target/remote", *remoteArg)
	}
	if plumbing && base.Target.Remote == "" {
		return rrInvalid(base, "send-pack requires a destination")
	}
	rrRemoteResolution(&base, !plumbing)
	for _, name := range []string{"all", "tags", "mirror", "prune", "follow-tags", "atomic", "set-upstream", "force-if-includes", "delete", "recurse-submodules"} {
		rrParameter(&base, p, name)
	}
	if p.Has("set-upstream") && !p.Has("dry-run") {
		base.Scopes = append(base.Scopes, model.ScopeConfig)
	}
	if p.Has("recurse-submodules") {
		o, _ := p.Option("recurse-submodules")
		if o.Value != "no" && o.Value != "off" && o.Value != "false" {
			base.Scopes = append(base.Scopes, model.ScopeSubmodule)
			rrUnresolved(&base, "target.remote", "recursive submodule pushes require submodule configuration", "git")
		}
	}
	base.Parameters["pushOptions"] = rrOptionValues(p, "push-option")
	rrExplicitExec(&base, p, "receive-pack", "exec")
	if o, ok := p.Option("signed"); ok && !o.Negated && o.Value != "false" && o.Value != "no" {
		base.Capabilities = append(base.Capabilities, model.CapabilityExternalExec)
	}
	// --mirror sets FORCE too; a later --no-force clears only FORCE.
	force := false
	for _, o := range p.Options {
		if o.Name == "force" || o.Name == "mirror" {
			force = !o.Negated
		}
	}
	if force {
		base.Parameters["forceMode"] = "unconditional"
	}
	leases := []string{}
	for _, o := range p.Options {
		if o.Name != "force-with-lease" {
			continue
		}
		if o.Negated {
			leases = nil
		} else {
			leases = append(leases, o.Value)
		}
	}
	if len(leases) > 0 {
		mode := "tracking"
		for _, value := range leases {
			if strings.Contains(value, ":") {
				mode = "explicit"
			}
		}
		if p.Has("force-if-includes") && mode == "tracking" {
			mode = "tracking-includes"
		}
		base.Parameters["leaseMode"], base.Parameters["leases"] = mode, leases
		if !force {
			base.Parameters["forceMode"] = "lease"
		}
	}
	if p.Has("dry-run") {
		base.Effect = model.EffectRead
	}
	if p.Has("stdin") {
		// send-pack's pkt-line mode shares stdin with a bidirectional protocol.
		if c.Invocation.Stdin.Status != model.StdinCaptured || p.Has("stateless-rpc") {
			base.Target.Refs = model.UnknownSet()
			base.Effect, base.Coverage, base.UnknownReason = model.EffectUnknown, model.CoverageOpaque, model.UnknownInputUnavailable
			rrUnresolved(&base, "input", "send-pack ref input is unavailable", "stdin")
			return []model.Operation{base}
		}
		args = append(append([]string(nil), args...), strings.Fields(string(c.Invocation.Stdin.Data))...)
	}
	if len(args) == 0 {
		op := base
		op.Parameters["refspecs"] = []string{}
		op.Target.Refs = model.UnknownSet()
		switch {
		case p.Has("mirror"):
			actions.Apply(&op, "remote.push.mirror")
		case p.Has("tags"):
			actions.Apply(&op, "remote.push.tags")
		case plumbing && !p.Has("all"):
			actions.Apply(&op, "remote.push.matching")
			op.Parameters["matching"] = true
		}
		if p.Has("dry-run") {
			op.Effect = model.EffectRead
		}
		rrUnresolved(&op, "target.refs", "push defaults and ref expansion require repository configuration or remote state", "git")
		return rrPushExtras(p, []model.Operation{op})
	}
	if p.Has("all") || p.Has("mirror") {
		return rrInvalid(base, "--all/--mirror cannot be combined with refspecs")
	}
	if p.Has("delete") && p.Has("tags") {
		return rrInvalid(base, "--delete cannot be combined with --tags")
	}
	// Keep each action separate so a mixed update/delete request cannot hide a delete.
	var ops []model.Operation
	negative := []string{}
	for _, raw := range args {
		if strings.HasPrefix(raw, "^") {
			negative = append(negative, raw)
		}
	}
	for i := 0; i < len(args); i++ {
		raw := args[i]
		if strings.HasPrefix(raw, "^") {
			continue
		}
		if raw == "tag" && i+1 < len(args) {
			i++
			raw = "refs/tags/" + args[i] + ":refs/tags/" + args[i]
		}
		op := base
		op.Parameters = rrCopyParameters(base.Parameters)
		op.Unresolved = append([]model.Unresolved(nil), base.Unresolved...)
		refspec := raw
		if p.Has("delete") {
			refspec = ":" + raw
		}
		op.Parameters["refspecs"] = append([]string{refspec}, negative...)
		if strings.HasPrefix(refspec, "+") {
			op.Parameters["forceMode"] = "unconditional"
			refspec = refspec[1:]
		}
		src, dst, colon := strings.Cut(refspec, ":")
		switch {
		case refspec == ":":
			actions.Apply(&op, "remote.push.matching")
			op.Parameters["matching"] = true
			op.Target.Refs = model.UnknownSet()
			rrUnresolved(&op, "target.refs", "matching push depends on refs present at the remote", "remote")
		case colon && src == "":
			actions.Apply(&op, "remote.push.delete")
			rrPushDestination(&op, dst)
		case colon:
			rrPushDestination(&op, dst)
		default:
			// A source-only refspec is not an explicit destination.
			op.Target.Refs = model.UnknownSet()
			rrUnresolved(&op, "target.refs", "source-only refspec destination requires local ref and push mapping resolution", "git")
		}
		if p.Has("dry-run") {
			op.Effect = model.EffectRead
		}
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		return rrInvalid(base, "push requires a positive refspec")
	}
	if p.Has("tags") {
		op := base
		actions.Apply(&op, "remote.push.tags")
		op.Parameters = rrCopyParameters(base.Parameters)
		op.Parameters["refspecs"] = []string{"refs/tags/*:refs/tags/*"}
		op.Target.Refs = rrSet([]string{"refs/tags/*"}, true)
		if p.Has("dry-run") {
			op.Effect = model.EffectRead
		}
		ops = append(ops, op)
	}
	if p.Has("prune") || p.Has("follow-tags") {
		for i := range ops {
			ops[i].Target.Refs.Complete = false
			rrUnresolved(&ops[i], "target.refs", "prune/follow-tags may affect additional refs based on remote state", "remote")
		}
	}
	return rrPushExtras(p, ops)
}

func rrPushExtras(p ParsedCommand, ops []model.Operation) []model.Operation {
	if p.Has("mirror") {
		return ops
	}
	for _, mode := range []struct{ flag, action string }{{"prune", "remote.push.delete"}, {"follow-tags", "remote.push.tags"}} {
		if !p.Has(mode.flag) || mode.flag == "follow-tags" && p.Has("tags") {
			continue
		}
		op := ops[0]
		actions.Apply(&op, mode.action)
		if p.Has("dry-run") {
			op.Effect = model.EffectRead
		}
		op.Parameters = rrCopyParameters(op.Parameters)
		op.Parameters["implicit"] = mode.flag
		op.Target.Refs = model.UnknownSet()
		op.Unresolved = append([]model.Unresolved(nil), op.Unresolved...)
		rrUnresolved(&op, "target.refs", "additional refs selected by "+mode.flag+" depend on remote advertisements", "remote")
		op.Editable = nil
		ops = append(ops, op)
	}
	return ops
}

func rrPushDestination(op *model.Operation, dst string) {
	if dst == "" {
		op.Target.Refs = model.UnknownSet()
		rrUnresolved(op, "target.refs", "empty destination requires source ref resolution", "git")
		return
	}
	op.Target.Refs = rrSet([]string{dst}, strings.Contains(dst, "*"))
	if !strings.HasPrefix(dst, "refs/") || strings.Contains(dst, "*") {
		op.Target.Refs.Complete = false
		rrUnresolved(op, "target.refs", "push destination needs namespace or pattern expansion", "git")
	}
}

func rrRemoteResolution(op *model.Operation, group bool) {
	reason := "remote URL and default remote require repository configuration"
	if group && op.Target.Remote != "" {
		reason = "remote may name a configured group or have multiple push URLs"
	}
	rrUnresolved(op, "target.remote", reason, "git")
}

func rrCopyParameters(parameters map[string]any) map[string]any {
	out := make(map[string]any, len(parameters))
	for key, value := range parameters {
		out[key] = value
	}
	return out
}
