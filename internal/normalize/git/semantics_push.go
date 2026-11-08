package git

import (
	"strings"

	"github.com/mkusaka/gthok/internal/model"
)

func init() { register("push", push) }

func push(c *Context) []model.Operation {
	op := model.Operation{Family: "remote.push", Action: "remote.push.update", Effect: model.EffectWrite,
		Coverage: model.CoverageConservative, Scopes: []model.Scope{model.ScopeRemoteRef},
		Capabilities: []model.Capability{model.CapabilityNetwork},
		Parameters:   map[string]any{"forceMode": "none", "leaseMode": "none", "dryRun": c.Parsed.Has("dry-run")}}
	args := c.Parsed.Values()
	if len(args) > 0 {
		op.Target.Remote, args = args[0], args[1:]
	} else if repo, ok := c.Parsed.Option("repo"); ok && !repo.Negated {
		op.Target.Remote = repo.Value
	} else {
		op.Unresolved = append(op.Unresolved, model.Unresolved{Field: "target.remote", Reason: "default push remote requires repository configuration", Source: "git"})
	}
	if c.Parsed.Has("force") {
		op.Parameters["forceMode"] = "unconditional"
	}
	if c.Parsed.Has("force-with-lease") {
		op.Parameters["leaseMode"] = "explicit"
	}
	op.Parameters["refspecs"] = args
	if len(args) == 0 {
		op.Target.Refs = model.UnknownSet()
	} else {
		refs := make([]string, 0, len(args))
		unknown := false
		complete := true
		kind := model.SetExact
		if len(args) > 1 {
			kind = model.SetSet
		}
		for _, refspec := range args {
			if strings.HasPrefix(refspec, "+") {
				op.Parameters["forceMode"] = "unconditional"
				refspec = refspec[1:]
			}
			if _, dst, ok := strings.Cut(refspec, ":"); ok {
				refspec = dst
			}
			if refspec == "" || refspec == "HEAD" {
				unknown = true
				continue
			}
			if strings.Contains(refspec, "*") {
				kind = model.SetPattern
			}
			refs = append(refs, refspec)
			if !strings.HasPrefix(refspec, "refs/") {
				complete = false
			}
		}
		if unknown || len(refs) == 0 {
			op.Target.Refs = model.UnknownSet()
		} else {
			op.Target.Refs = &model.TargetSet{Kind: kind, Values: refs, Complete: complete}
			if !complete {
				op.Unresolved = append(op.Unresolved, model.Unresolved{Field: "target.refs", Reason: "unqualified push destinations require ref namespace resolution", Source: "git"})
			}
		}
	}
	// shortcut: push modes and ref expansion stay conservative until the remote semantics adapter lands.
	if op.Target.Refs.Kind == model.SetUnknown || c.Parsed.Has("all") || c.Parsed.Has("tags") || c.Parsed.Has("mirror") || c.Parsed.Has("follow-tags") || c.Parsed.Has("prune") {
		op.Target.Refs = model.UnknownSet()
		op.Unresolved = append(op.Unresolved, model.Unresolved{Field: "target.refs", Reason: "push ref expansion requires local configuration or remote state", Source: "git"})
	}
	return []model.Operation{op}
}
